package kane

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
)

func parseFile(t *testing.T, name string) *Parser {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	p := &Parser{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		p.Line(sc.Bytes())
	}
	return p
}

func TestParsePassed(t *testing.T) {
	p := parseFile(t, "passed.ndjson")
	var run core.Run
	p.Fill(&run)
	if run.CLIVersion != "0.10.0" || len(run.Steps) != 3 || run.Credits != 6.2 || run.DurationS != 21.5 {
		t.Fatalf("run %+v", run)
	}
	for _, s := range run.Steps {
		if s.Status != "done" {
			t.Errorf("step %+v", s)
		}
	}
	if run.FinalState["observed"] != "1 item" || run.TestURL == "" || run.BugVerdict != nil {
		t.Errorf("run end not read: %+v", run)
	}
}

func TestParseFailed(t *testing.T) {
	var seen int
	p := parseFile(t, "failed.ndjson")
	p.OnStep = func([]core.Step) { seen++ }
	var run core.Run
	p.Fill(&run)
	if len(run.Steps) != 4 {
		t.Fatalf("want 4 steps (repeats and skips counted by order), got %+v", run.Steps)
	}
	last := run.Steps[3]
	if last.N != 4 || last.Status != "failed" || !strings.Contains(last.Remark, "₹498") {
		t.Errorf("last step %+v", last)
	}
	if run.Reason != "assertion_failed: @ step 7" || run.Credits != 11.9 || run.FinalState["observed"] != "₹498" {
		t.Errorf("run end %+v", run)
	}
	v := run.BugVerdict
	if v == nil || !v.Confirmed || v.Confidence != "0.92" || v.Severity != "high" {
		t.Errorf("verdict %+v", v)
	}
}

func TestParseExit2(t *testing.T) {
	p := parseFile(t, "exit2.ndjson")
	if len(p.Steps) != 0 || p.RunEnd != nil || len(p.Errors) != 1 {
		t.Errorf("parser %+v", p)
	}
}

func TestParseTestMD(t *testing.T) {
	p := parseFile(t, "testmd.ndjson")
	var run core.Run
	p.Fill(&run)
	if len(run.Steps) != 1 || run.Steps[0].Status != "done" || run.Steps[0].Remark != "Remove an item and check the total" {
		t.Errorf("steps %+v", run.Steps)
	}
	if !p.TestMD.Replayed() || !p.TestMD.Done || p.TestMD.OverallStatus != "passed" {
		t.Errorf("testmd %+v", p.TestMD)
	}
	if run.DurationS != 13.4 || run.Credits != 2.5 || run.TestURL != "https://example.test/share/1" {
		t.Errorf("run %+v", run)
	}
}

func TestParseGarbage(t *testing.T) {
	p := &Parser{}
	for _, l := range []string{"", "not json", "{broken", `["array"]`, `{"type":"run_end","status":`, `{"type":42}`} {
		p.Line([]byte(l))
	}
	if len(p.Steps) != 0 || p.RunEnd != nil {
		t.Errorf("garbage produced %+v", p)
	}
}

func TestLineWriterSplitsAcrossWrites(t *testing.T) {
	var got []string
	w := &lineWriter{fn: func(b []byte) { got = append(got, string(b)) }}
	w.Write([]byte("ab"))
	w.Write([]byte("c\nde\nf"))
	w.flush()
	if strings.Join(got, "|") != "abc|de|f" {
		t.Errorf("got %q", got)
	}
}

func buildPack(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range entries {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func TestReadPack(t *testing.T) {
	dir := t.TempDir()
	pack := filepath.Join(dir, "run.evidence")
	dest := filepath.Join(dir, "artifacts", "r_1", "run_1")
	buildPack(t, pack, map[string]string{
		"run.yaml":                                   "id: x\n",
		"tests/t1/result.yaml":                       "status: failed\n",
		"tests/t1/steps/1-nav/screenshot.png":        "PNG1",
		"tests/t1/steps/2-click/screenshot.png":      "PNG2",
		"tests/t1/steps/2-click/annotated.png":       "PNG2A",
		"tests/t1/steps/10-verify/screenshot.png":    "PNG10",
		"tests/t1/steps/10-verify/failure.yaml":      "reason: total was ₹498\n",
		"tests/t1/logs/1-console.ndjson":             "{\"level\":\"error\",\"text\":\"TypeError: x is undefined\"}\n{\"level\":\"info\",\"text\":\"hi\"}\nnot json\n",
		"../../escape.png":                           "EVIL",
		"tests/t1/steps/3-x/../../../../../evil.png": "EVIL",
		"/abs/steps/4-a/screenshot.png":              "EVIL",
	})
	ev, err := ReadPack(pack, dest, "/artifacts/r_1/run_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Shots) != 3 || ev.Shots[0].Step != 1 || ev.Shots[2].Step != 10 {
		t.Fatalf("shots %+v", ev.Shots)
	}
	if !ev.Shots[2].Failed || ev.Shots[0].Failed {
		t.Errorf("failed marks %+v", ev.Shots)
	}
	if ev.Shots[1].AnnotatedURL != "/artifacts/r_1/run_1/step-2-annotated.png" || ev.Shots[1].URL != "/artifacts/r_1/run_1/step-2.png" {
		t.Errorf("urls %+v", ev.Shots[1])
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "step-10.png")); string(b) != "PNG10" {
		t.Errorf("step-10.png = %q", b)
	}
	if !strings.Contains(ev.FailureText, "₹498") || len(ev.ConsoleErrors) != 1 {
		t.Errorf("failure %q console %v", ev.FailureText, ev.ConsoleErrors)
	}
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && p != pack {
			if b, _ := os.ReadFile(p); string(b) == "EVIL" {
				t.Errorf("a traversal entry was written to %s", p)
			}
		}
		return nil
	})
	for _, bad := range []string{"../../escape.png", "a/../../b", "/abs", `a\b`, "C:/x"} {
		if safeName(bad) {
			t.Errorf("safeName(%q) = true", bad)
		}
	}
}

func TestFindPack(t *testing.T) {
	dir := t.TempDir()
	evDir := filepath.Join(dir, ".testmuai", "evidence")
	os.MkdirAll(evDir, 0o755)
	old := filepath.Join(evDir, "old.evidence")
	os.WriteFile(old, []byte("x"), 0o644)
	os.Chtimes(old, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))
	start := time.Now()
	if got := FindPack("", "", dir, start); got != "" {
		t.Errorf("an old pack was picked: %s", got)
	}
	fresh := filepath.Join(evDir, "new.evidence")
	os.WriteFile(fresh, []byte("x"), 0o644)
	if got := FindPack("", "", dir, start); got != fresh {
		t.Errorf("got %q want %q", got, fresh)
	}
	hinted := filepath.Join(dir, "hinted.evidence")
	os.WriteFile(hinted, []byte("x"), 0o644)
	stderr := "done\nevidence: view locally with `kane-cli evidence serve " + hinted + "`\n"
	if got := FindPack(stderr, "", dir, start); got != hinted {
		t.Errorf("hint ignored: %q", got)
	}
}

// fakeKane writes a shell script that behaves like kane-cli for one case.
func fakeKane(t *testing.T, stdout string, code int) string {
	t.Helper()
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("needs /bin/sh")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "out.ndjson")
	os.WriteFile(out, []byte(stdout), 0o644)
	script := filepath.Join(dir, "kane")
	body := "#!/bin/sh\nif [ \"$2\" = \"--help\" ]; then echo '--bug-detection'; exit 0; fi\ncat " + out + "\nexit " + string(rune('0'+code)) + "\n"
	os.WriteFile(script, []byte(body), 0o755)
	return script
}

func TestCLIRunExitCodes(t *testing.T) {
	failed, _ := os.ReadFile("testdata/failed.ndjson")
	exit2, _ := os.ReadFile("testdata/exit2.ndjson")
	cases := []struct {
		name   string
		out    string
		code   int
		status core.RunStatus
		errHas string
	}{
		{"failed", string(failed), 1, core.RunFailed, ""},
		{"exit 2 no steps", string(exit2), 2, core.RunErrored, "kane-cli whoami"},
		{"timeout", "", 3, core.RunTimeout, "timeout"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cli := &CLI{Bin: fakeKane(t, c.out, c.code), WorkDir: t.TempDir(), TimeoutS: 30, MaxSteps: 5, BugDetection: "continue"}
			res := cli.Run(context.Background(), Request{Objective: "Go to http://x.", StartURL: "http://x"}, nil)
			if res.Run.Status != c.status || !strings.Contains(res.Run.Error, c.errHas) {
				t.Errorf("status %s error %q", res.Run.Status, res.Run.Error)
			}
		})
	}
	cli := &CLI{Bin: filepath.Join(t.TempDir(), "missing"), WorkDir: t.TempDir(), TimeoutS: 5}
	if res := cli.Run(context.Background(), Request{Objective: "x"}, nil); res.Run.Error != RemedyInstall {
		t.Errorf("missing binary: %q", res.Run.Error)
	}
	cli = &CLI{Bin: fakeKane(t, "", 0), WorkDir: t.TempDir(), TimeoutS: 5, MaxSteps: 3, BugDetection: "continue", Headless: true}
	args := strings.Join(cli.Args(context.Background(), Request{Objective: "Go", StartURL: "http://x", Name: "fp-1"}), " ")
	if !strings.Contains(args, "--bug-detection continue") || !strings.Contains(args, "--name fp-1") || !strings.Contains(args, "--headless") {
		t.Errorf("args %s", args)
	}
}

func TestCLIKillsGroupOnCancel(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "kane")
	pidFile := filepath.Join(dir, "child.pid")
	os.WriteFile(script, []byte("#!/bin/sh\nsleep 60 &\necho $! > "+pidFile+"\nwait\n"), 0o755)
	cli := &CLI{Bin: script, WorkDir: dir, TimeoutS: 30}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	t0 := time.Now()
	res := cli.Run(ctx, Request{Objective: "x"}, nil)
	if time.Since(t0) > 10*time.Second || res.Run.Status != core.RunTimeout {
		t.Fatalf("status %s after %s", res.Run.Status, time.Since(t0))
	}
	b, _ := os.ReadFile(pidFile)
	pid := strings.TrimSpace(string(b))
	if pid == "" {
		return
	}
	// SIGKILL is delivered asynchronously: Wait returns once the shell is
	// reaped, but the grandchild can still be tearing down for a moment.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat("/proc/" + pid); err != nil {
			return // gone
		}
		if st, _ := os.ReadFile("/proc/" + pid + "/stat"); strings.Contains(string(st), ") Z") {
			return // zombie, waiting to be reaped
		}
		if time.Now().After(deadline) {
			t.Fatalf("grandchild %s still running", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestReadPackKane010 uses the layout Kane CLI 0.10.3 really writes: JPEG
// screenshots, a step.json per step and one failure.yaml at the root.
func TestReadPackKane010(t *testing.T) {
	dir := t.TempDir()
	pack := filepath.Join(dir, "run.evidence")
	buildPack(t, pack, map[string]string{
		"failure.yaml":                        "totals:\n  failures: 1\nfailures:\n  - step: 3\n    reason: total stayed at 469\n",
		"run.yaml":                            "status: finalized\n",
		"tests/t1/steps/1-0-1/screenshot.jpg": "JPG1",
		"tests/t1/steps/1-0-1/step.json":      `{"kind":"navigate","status":"passed","ordinal":1}`,
		"tests/t1/steps/3-0-3/screenshot.jpg": "JPG3",
		"tests/t1/steps/3-0-3/step.json":      `{"kind":"assert","status":"failed","ordinal":3}`,
	})
	dest := filepath.Join(dir, "out")
	ev, err := ReadPack(pack, dest, "/artifacts/r_1/run_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.Shots) != 2 || ev.Shots[0].URL != "/artifacts/r_1/run_1/step-1.jpg" || ev.Shots[1].Failed != true || ev.Shots[0].Failed {
		t.Fatalf("shots %+v", ev.Shots)
	}
	if !strings.Contains(ev.FailureText, "total stayed at 469") {
		t.Fatalf("failure text %q", ev.FailureText)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "step-3.jpg")); string(b) != "JPG3" {
		t.Fatalf("step-3.jpg holds %q", b)
	}
	// A passing run's root failure.yaml lists nothing and is not a failure.
	buildPack(t, pack, map[string]string{"failure.yaml": "totals:\n  failures: 0\nfailures: []\n"})
	if ev, _ := ReadPack(pack, dest, "/a"); ev.FailureText != "" {
		t.Fatalf("passing run got failure text %q", ev.FailureText)
	}
}

// TestFindTestFile picks the recording this run wrote, even when Kane had to
// add a suffix because an older recording held the name.
func TestFindTestFile(t *testing.T) {
	work := t.TempDir()
	dir := filepath.Join(work, ".testmuai", "tests")
	os.MkdirAll(dir, 0o755)
	old := filepath.Join(dir, "fp-1-r1_test.md")
	os.WriteFile(old, []byte("old check"), 0o644)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(old, past, past)
	start := time.Now()
	if got := FindTestFile(work, "fp-1-r1", start); got != "" {
		t.Fatalf("an older recording was returned: %s", got)
	}
	fresh := filepath.Join(dir, "fp-1-r1-2_test.md")
	os.WriteFile(fresh, []byte("this run"), 0o644)
	if got := FindTestFile(work, "fp-1-r1", start); got != fresh {
		t.Fatalf("got %q, want %q", got, fresh)
	}
	os.Remove(fresh)
	os.WriteFile(old, []byte("rewritten by this run"), 0o644)
	if got := FindTestFile(work, "fp-1-r1", start); got != old {
		t.Fatalf("got %q, want %q", got, old)
	}
}

func TestArgsTestFileIsAbsolute(t *testing.T) {
	c := &CLI{WorkDir: ".fixpoint/kane", TimeoutS: 60}
	args := c.Args(context.Background(), Request{TestFile: ".fixpoint/kane/.testmuai/tests/fp-1-v1_test.md"})
	if len(args) < 3 || args[0] != "testmd" || !filepath.IsAbs(args[2]) {
		t.Fatalf("args %q: want testmd run <absolute path>, since Kane runs inside WorkDir", args)
	}
}
