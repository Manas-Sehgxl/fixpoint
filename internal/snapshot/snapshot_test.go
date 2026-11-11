package snapshot

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files, err := textFiles(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for k, v := range files {
		out[k] = string(v)
	}
	return out
}

func TestUnified(t *testing.T) {
	cases := []struct {
		name, old, new, want string
	}{
		{"first line", "a\nb\nc\n", "A\nb\nc\n", "--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n-a\n+A\n b\n c\n"},
		{"last line", "a\nb\nc\n", "a\nb\nC\n", "--- a/f\n+++ b/f\n@@ -1,3 +1,3 @@\n a\n b\n-c\n+C\n"},
		{"no trailing newline added", "a\nb", "a\nb\n", "--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+b\n"},
		{"both without newline", "x\ny", "x\nz", "--- a/f\n+++ b/f\n@@ -1,2 +1,2 @@\n x\n-y\n\\ No newline at end of file\n+z\n\\ No newline at end of file\n"},
		{"two hunks",
			"1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n11\n12\n", "1\nX\n3\n4\n5\n6\n7\n8\n9\n10\nY\n12\n",
			"--- a/f\n+++ b/f\n@@ -1,5 +1,5 @@\n 1\n-2\n+X\n 3\n 4\n 5\n@@ -8,5 +8,5 @@\n 8\n 9\n 10\n-11\n+Y\n 12\n"},
		{"pure insert", "a\nb\n", "a\nnew\nb\n", "--- a/f\n+++ b/f\n@@ -1,2 +1,3 @@\n a\n+new\n b\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, _ := Unified("f", c.old, c.new, true, true)
			if got != c.want {
				t.Errorf("got\n%s\nwant\n%s", got, c.want)
			}
		})
	}
	if p, _, _ := Unified("f", "same\n", "same\n", true, true); p != "" {
		t.Errorf("equal texts gave a patch: %q", p)
	}
	p, adds, dels := Unified("n", "", "one\ntwo\n", false, true)
	if want := "--- /dev/null\n+++ b/n\n@@ -0,0 +1,2 @@\n+one\n+two\n"; p != want || adds != 2 || dels != 0 {
		t.Errorf("added file: %q %d %d", p, adds, dels)
	}
	p, _, dels = Unified("d", "gone\n", "", true, false)
	if want := "--- a/d\n+++ /dev/null\n@@ -1,1 +0,0 @@\n-gone\n"; p != want || dels != 1 {
		t.Errorf("deleted file: %q", p)
	}
}

func TestSnapshotDiffRestore(t *testing.T) {
	base := t.TempDir()
	live := filepath.Join(base, "app")
	snap := filepath.Join(base, "snap")
	orig := map[string]string{
		"index.html": "<h1>Tapri</h1>\n",
		"js/cart.js": "export const a = 1;\nexport const b = 2;",
		"js/gone.js": "old\n",
		"README":     "readme\n",
	}
	writeTree(t, live, orig)
	writeTree(t, live, map[string]string{"node_modules/x.js": "skip\n", ".git/HEAD": "ref\n"})
	if err := os.WriteFile(filepath.Join(live, "logo.bin"), []byte{1, 0, 2}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Take(live, snap); err != nil {
		t.Fatal(err)
	}
	snapFiles := readTree(t, snap)
	if len(snapFiles) != len(orig) {
		t.Fatalf("snapshot holds %v, want only the text files", snapFiles)
	}

	writeTree(t, live, map[string]string{"js/cart.js": "export const a = 1;\nexport const b = 3;", "js/new.js": "fresh\n"})
	os.Remove(filepath.Join(live, "js/gone.js"))

	diffs, err := Diff(snap, live)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range diffs {
		got[d.Path] = d.Change
	}
	want := map[string]string{"js/cart.js": "modified", "js/new.js": "added", "js/gone.js": "deleted"}
	if len(got) != len(want) {
		t.Fatalf("diffs %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %s, want %s", k, got[k], v)
		}
	}

	// The patch must apply with git to the original tree.
	if git, err := exec.LookPath("git"); err == nil {
		check := filepath.Join(base, "check")
		writeTree(t, check, orig)
		patch := filepath.Join(base, "fix.patch")
		os.WriteFile(patch, []byte(JoinPatches(diffs)), 0o644)
		cmd := exec.Command(git, "apply", "--unsafe-paths", "--directory=.", patch)
		cmd.Dir = check
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git apply failed: %v\n%s\n%s", err, out, JoinPatches(diffs))
		}
		after := readTree(t, check)
		if after["js/cart.js"] != "export const a = 1;\nexport const b = 3;" || after["js/new.js"] != "fresh\n" {
			t.Errorf("git apply produced %v", after)
		}
		if _, ok := after["js/gone.js"]; ok {
			t.Error("git apply did not delete js/gone.js")
		}
	}

	if err := Restore(snap, live); err != nil {
		t.Fatal(err)
	}
	restored := readTree(t, live)
	for k, v := range orig {
		if restored[k] != v {
			t.Errorf("after restore %s = %q, want %q", k, restored[k], v)
		}
	}
	if _, ok := restored["js/new.js"]; ok {
		t.Error("restore kept the added file")
	}
	if b, _ := os.ReadFile(filepath.Join(live, "node_modules/x.js")); !strings.Contains(string(b), "skip") {
		t.Error("restore touched node_modules")
	}
	if d, _ := Diff(snap, live); len(d) != 0 {
		t.Errorf("diff after restore: %v", d)
	}
}

func TestMyersLarge(t *testing.T) {
	var a, b strings.Builder
	for i := 0; i < 3000; i++ {
		a.WriteString("line\n")
		if i%500 == 0 {
			b.WriteString("changed\n")
		} else {
			b.WriteString("line\n")
		}
	}
	_, adds, dels := Unified("f", a.String(), b.String(), true, true)
	if adds != 6 || dels != 6 {
		t.Errorf("adds %d dels %d, want 6 and 6", adds, dels)
	}
}
