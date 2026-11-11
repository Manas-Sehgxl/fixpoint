package stub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const scenariosJSON = `{"scenarios":[
 {"id":"stale-total","report_keywords":["remov","total"],"title":"Total stays the same after removing an item",
  "objective":"Go to {{url}}. Click the \"Add to cart\" button on the \"Masala Chai\" card. Click \"Remove\". Store the total as 'observed'. Verify the order total shows ₹0.",
  "expected":"The total drops by the removed item's price.","question":"Which item did you remove?",
  "file":"js/cart.js","buggy":"  renderList();\n}","fixed":"  renderList();\n  recomputeTotals();\n}",
  "observed":"The total still shows ₹498 after the item is removed.","root_cause":"removeItem() re-renders the list but never recomputes the totals."}
]}`

func withScenarios(t *testing.T) (scenariosPath, targetDir string) {
	t.Helper()
	dir := t.TempDir()
	scenariosPath = filepath.Join(dir, "scenarios.json")
	if err := os.WriteFile(scenariosPath, []byte(scenariosJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	targetDir = filepath.Join(dir, "shop")
	if err := os.MkdirAll(filepath.Join(targetDir, "js"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "js", "cart.js"),
		[]byte("export function removeItem(id) {\n  items = items.filter(i => i.id !== id);\n  renderList();\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return scenariosPath, targetDir
}

func TestMatchObjectiveAndReport(t *testing.T) {
	scs := LoadScenarios(mustWrite(t, scenariosJSON))
	url := "http://localhost:4000"
	objective := Resolve(scs.Scenarios[0].Objective, url)
	if got := scs.MatchObjective(objective, url); got == nil || got.ID != "stale-total" {
		t.Fatalf("exact objective did not match: %+v", got)
	}
	spaced := strings.ReplaceAll(objective, ". ", ".\n  ")
	if got := scs.MatchObjective(spaced, url+"/"); got == nil {
		t.Error("whitespace differences should not stop a match")
	}
	if got := scs.MatchObjective(objective, ""); got == nil {
		t.Error("the URL in the objective should be enough to match")
	}
	if got := scs.MatchObjective("Go to http://localhost:4000. Verify the page shows the menu.", url); got != nil {
		t.Error("the generic objective matched a scenario")
	}
	if got := scs.MatchReport("i REMOVEd an item and the Total did not change"); got == nil || got.ID != "stale-total" {
		t.Error("keywords should match regardless of case")
	}
	if got := scs.MatchReport("the total is odd"); got != nil {
		t.Error("a partial keyword set matched")
	}
}

func mustWrite(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "scenarios.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStubTriage(t *testing.T) {
	scs := LoadScenarios(mustWrite(t, scenariosJSON))
	prompt := "FIXPOINT-TASK: triage\n\nlive app at http://localhost:4000.\n<report>\nI removed an item and the total stayed the same\n</report>\n"
	out := stubTriage(scs, prompt)
	if out["decision"] != "check" || !strings.Contains(out["objective"].(string), "http://localhost:4000") {
		t.Fatalf("triage %+v", out)
	}
	if strings.Contains(out["objective"].(string), "{{url}}") {
		t.Error("{{url}} was left in the objective")
	}
	short := stubTriage(scs, "FIXPOINT-TASK: triage\n\nlive app at http://localhost:4000.\n<report>\nbroken\n</report>\n")
	if short["decision"] != "ask" || short["question"] != askQuestion {
		t.Errorf("a four-word report should be asked about: %+v", short)
	}
	vague := stubTriage(scs, "FIXPOINT-TASK: triage\n\nlive app at http://localhost:4000.\n<report>\nthe checkout page felt wrong to me today\n</report>\n")
	if vague["decision"] != "check" || !strings.Contains(vague["objective"].(string), "shows the menu") {
		t.Errorf("a vague report should get the passing check: %+v", vague)
	}
}

func fixPrompt(objective, report string) string {
	return "FIXPOINT-TASK: fix\n\nlive app at http://localhost:4000.\n<report>\n" + report +
		"\n</report>\n\n## The check Kane runs\n" + objective + "\nCorrect behaviour: it works\n"
}

func TestStubFixPatchesTheFile(t *testing.T) {
	scPath, dir := withScenarios(t)
	scs := LoadScenarios(scPath)
	objective := Resolve(scs.Scenarios[0].Objective, "http://localhost:4000")
	cart := filepath.Join(dir, "js", "cart.js")

	if !scs.Scenarios[0].StillBuggy(dir) {
		t.Fatal("the seed file should hold the buggy text")
	}
	out, errMsg := stubFix(scs, dir, fixPrompt(objective, "I removed an item and the total stayed the same"))
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	if out["action"] != "patched" || out["root_cause"] == "" {
		t.Fatalf("fix %+v", out)
	}
	b, _ := os.ReadFile(cart)
	if !strings.Contains(string(b), "recomputeTotals();") {
		t.Fatalf("the file was not patched: %s", b)
	}
	if scs.Scenarios[0].StillBuggy(dir) {
		t.Error("the buggy text survived the patch")
	}

	// A stubborn report makes a harmless edit instead, so Kane keeps failing.
	scPath, dir = withScenarios(t)
	scs = LoadScenarios(scPath)
	out, errMsg = stubFix(scs, dir, fixPrompt(objective, "removing an item leaves the total alone [stub:stubborn]"))
	if errMsg != "" || out["action"] != "patched" {
		t.Fatalf("stubborn fix %+v %s", out, errMsg)
	}
	if !scs.Scenarios[0].StillBuggy(dir) {
		t.Error("the stubborn edit removed the bug")
	}
	out, _ = stubFix(scs, dir, fixPrompt(objective, "total and remove [stub:giveup]"))
	if out["action"] != "give_up" {
		t.Errorf("giveup fix %+v", out)
	}
	if _, errMsg = stubFix(scs, dir, fixPrompt("Go to http://localhost:4000. Verify something else.", "x")); errMsg == "" {
		t.Error("an unmatched check should be an agent error")
	}
}

func TestSteps(t *testing.T) {
	got := steps("Go to http://x/. Click \"Add\". Verify the total shows ₹249.")
	if len(got) != 3 || !strings.HasPrefix(got[0], "Go to") || !strings.HasPrefix(got[2], "Verify") {
		t.Errorf("steps %q", got)
	}
	if len(steps("")) != 1 {
		t.Error("an empty objective should still give one step")
	}
}

func TestGeneratedPNG(t *testing.T) {
	b := generatedPNG()
	if len(b) < 100 || string(b[1:4]) != "PNG" {
		t.Errorf("not a PNG: %d bytes", len(b))
	}
}

func TestTestMDRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := writeTestMD(dir, "fp-1-r1", "Go to http://x/. Verify the total shows ₹249.", "http://x/"); err != nil {
		t.Fatal(err)
	}
	obj, url := readTestMD(filepath.Join(dir, ".testmuai", "tests", "fp-1-r1_test.md"))
	if obj != "Go to http://x/. Verify the total shows ₹249." || url != "http://x/" {
		t.Errorf("read back %q %q", obj, url)
	}
}
