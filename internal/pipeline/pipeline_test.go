package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/agent"
	"github.com/Manas-Sehgxl/fixpoint/internal/core"
	"github.com/Manas-Sehgxl/fixpoint/internal/events"
	"github.com/Manas-Sehgxl/fixpoint/internal/kane"
	"github.com/Manas-Sehgxl/fixpoint/internal/snapshot"
	"github.com/Manas-Sehgxl/fixpoint/internal/store"
)

const seed = "export function total(items) {\n  return BUG;\n}\n"

// fakeRunner decides each run with a function of the objective and the
// target's app.js.
type fakeRunner struct {
	dir    string
	mu     sync.Mutex
	calls  []kane.Request
	decide func(n int, req kane.Request, app string) core.RunStatus
}

func (f *fakeRunner) Run(ctx context.Context, req kane.Request, onStep func([]core.Step)) kane.Result {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	n := len(f.calls)
	f.mu.Unlock()
	b, _ := os.ReadFile(filepath.Join(f.dir, "app.js"))
	st := f.decide(n, req, string(b))
	steps := []core.Step{{N: 1, Status: "done", Remark: "Navigate"}}
	if onStep != nil {
		onStep([]core.Step{{N: 1, Status: "running", Remark: "Navigate"}})
		onStep(steps)
	}
	run := core.Run{Status: st, Steps: steps, Summary: "saw " + string(st), FinalState: map[string]any{"observed": "x"}}
	switch st {
	case core.RunFailed:
		run.ExitCode, run.Reason = 1, "assertion_failed: @ step 1"
	case core.RunErrored:
		run.ExitCode, run.Error, run.Steps = 2, kane.RemedyNotStarted, []core.Step{}
	}
	now := time.Now().UTC()
	run.EndedAt = &now
	return kane.Result{Run: run}
}

// failsWhile fails any check while app.js contains marker.
func failsWhile(marker string) func(int, kane.Request, string) core.RunStatus {
	return func(_ int, _ kane.Request, app string) core.RunStatus {
		if strings.Contains(app, marker) {
			return core.RunFailed
		}
		return core.RunPassed
	}
}

type fakeAgent struct {
	dir    string
	mu     sync.Mutex
	fixes  []agent.FixInput
	triage func(in agent.TriageInput) agent.Triage
	fix    func(n int, in agent.FixInput, app string) (agent.Fix, string) // returns the new app.js ("" = unchanged)
}

func (a *fakeAgent) Name() string { return "Fake agent" }
func (a *fakeAgent) Health(context.Context) core.ToolInfo {
	return core.ToolInfo{Name: "Fake agent", Found: true}
}
func (a *fakeAgent) Triage(_ context.Context, in agent.TriageInput) (agent.Triage, core.AgentCall, error) {
	if a.triage != nil {
		return a.triage(in), core.AgentCall{Name: "Fake agent"}, nil
	}
	return agent.Triage{Decision: "check", Title: "Total is wrong", Objective: "Go to http://app.test/. Verify the total.",
		Expected: "Right total.", Question: "Which item?"}, core.AgentCall{Name: "Fake agent"}, nil
}
func (a *fakeAgent) Fix(_ context.Context, in agent.FixInput) (agent.Fix, core.AgentCall, error) {
	a.mu.Lock()
	a.fixes = append(a.fixes, in)
	n := len(a.fixes)
	a.mu.Unlock()
	p := filepath.Join(a.dir, "app.js")
	b, _ := os.ReadFile(p)
	f, next := a.fix(n, in, string(b))
	if next != "" {
		os.WriteFile(p, []byte(next), 0o644)
	}
	return f, core.AgentCall{Name: "Fake agent", CostUSD: 0.01}, nil
}

func patched(summary string) agent.Fix {
	return agent.Fix{Action: "patched", RootCause: "app.js returns BUG.", Summary: summary}
}

type harness struct {
	t      *testing.T
	dir    string
	data   string
	st     *store.Store
	w      *Worker
	runner *fakeRunner
	agent  *fakeAgent
	cancel context.CancelFunc
	done   chan struct{}
}

func newHarness(t *testing.T, decide func(int, kane.Request, string) core.RunStatus, fix func(int, agent.FixInput, string) (agent.Fix, string), mutate func(*Options)) *harness {
	t.Helper()
	root := t.TempDir()
	h := &harness{t: t, dir: filepath.Join(root, "app"), data: filepath.Join(root, "data")}
	os.MkdirAll(h.dir, 0o755)
	os.WriteFile(filepath.Join(h.dir, "app.js"), []byte(seed), 0o644)
	os.WriteFile(filepath.Join(h.dir, "index.html"), []byte("<h1>shop</h1>\n"), 0o644)
	var err error
	h.st, err = store.Open(filepath.Join(h.data, "data"))
	if err != nil {
		t.Fatal(err)
	}
	h.runner = &fakeRunner{dir: h.dir, decide: decide}
	h.agent = &fakeAgent{dir: h.dir, fix: fix}
	o := Options{Store: h.st, Bus: events.New(h.st), Runner: h.runner, Agent: h.agent,
		Target: Target{Name: "Shop", URL: "http://app.test", Dir: h.dir}, MaxAttempts: 3, MaxInfoRounds: 2,
		ArtifactsDir: filepath.Join(h.data, "artifacts"), SnapshotsDir: filepath.Join(h.data, "snapshots")}
	if mutate != nil {
		mutate(&o)
	}
	h.w = New(o)
	return h
}

func (h *harness) start() {
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.done = make(chan struct{})
	h.w.Recover(ctx)
	go func() { h.w.Run(ctx); close(h.done) }()
	h.t.Cleanup(h.stop)
}

func (h *harness) stop() {
	if h.cancel != nil {
		h.cancel()
		<-h.done
		h.cancel = nil
	}
}

func (h *harness) submit(text string) string {
	r, err := h.w.Submit(core.NewReportRequest{Text: text})
	if err != nil {
		h.t.Fatal(err)
	}
	return r.ID
}

func (h *harness) wait(id string, want core.Status) core.Report {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := h.st.Get(id)
		if r.Status == want && h.w.Queue().ActiveReportID != id {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	r, _ := h.st.Get(id)
	h.t.Fatalf("report stayed %s (want %s); problem %+v attempts %+v", r.Status, want, r.Problem, r.Attempts)
	return r
}

// waitStatus waits for a status without requiring the worker to be idle.
func (h *harness) waitStatus(id string, want core.Status) core.Report {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := h.st.Get(id)
		if r.Status == want {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	r, _ := h.st.Get(id)
	h.t.Fatalf("report stayed %s (want %s)", r.Status, want)
	return r
}

func (h *harness) app() string {
	b, _ := os.ReadFile(filepath.Join(h.dir, "app.js"))
	return string(b)
}

func eventTypes(st *store.Store, id string) []string {
	d, _ := st.Detail(id)
	var out []string
	for _, e := range d.Events {
		out = append(out, e.Type)
	}
	return out
}

func TestProvenOnFirstAttempt(t *testing.T) {
	h := newHarness(t, failsWhile("BUG"), func(n int, in agent.FixInput, app string) (agent.Fix, string) {
		return patched("Return the sum."), strings.Replace(app, "BUG", "sum(items)", 1)
	}, nil)
	h.start()
	id := h.submit("The total is wrong after removing an item")
	r := h.wait(id, core.StatusProven)
	if len(r.Attempts) != 1 || r.Attempts[0].Status != core.AttemptVerified || len(r.Attempts[0].Files) != 1 {
		t.Fatalf("attempts %+v", r.Attempts)
	}
	if r.Proof == nil || r.Proof.Attempt != 1 || len(r.Proof.Files) != 1 || r.Proof.RedRunID == "" || r.Proof.GreenRunID == "" {
		t.Fatalf("proof %+v", r.Proof)
	}
	if r.Check.WrittenBy != "Fake agent" || r.Check.StartURL != "http://app.test/" {
		t.Errorf("check %+v", r.Check)
	}
	patch, err := os.ReadFile(filepath.Join(h.data, "artifacts", id, "fix.patch"))
	if err != nil || !strings.Contains(string(patch), "+  return sum(items);") {
		t.Errorf("patch file %q %v", patch, err)
	}
	if tests := h.st.Suite(); len(tests) != 1 || tests[0].ReportID != id || tests[0].LastRun == nil {
		t.Errorf("suite %+v", tests)
	}
	if snapshot.Exists(filepath.Join(h.data, "snapshots", id)) {
		t.Error("snapshots were kept after proof")
	}
	types := strings.Join(eventTypes(h.st, id), " ")
	for _, want := range []string{"report.created", "check.written", "run.started", "run.finished", "attempt.started", "attempt.patched", "attempt.finished", "report.proven"} {
		if !strings.Contains(types, want) {
			t.Errorf("no %s event in %s", want, types)
		}
	}
	if strings.Contains(types, "run.step") {
		t.Error("run.step events were persisted")
	}
}

func TestProvenOnSecondAttempt(t *testing.T) {
	h := newHarness(t, failsWhile("BUG"), func(n int, in agent.FixInput, app string) (agent.Fix, string) {
		if n == 1 {
			return patched("Tidy."), app + "// tidy\n"
		}
		return patched("Return the sum."), strings.Replace(app, "BUG", "sum(items)", 1)
	}, nil)
	h.start()
	id := h.submit("Total is wrong")
	r := h.wait(id, core.StatusProven)
	if len(r.Attempts) != 2 || r.Attempts[0].Status != core.AttemptStillFailing || r.Attempts[1].Status != core.AttemptVerified {
		t.Fatalf("attempts %+v", r.Attempts)
	}
	second := h.agent.fixes[1]
	if len(second.Previous) != 1 || second.Evidence.Kind != core.RunVerify || second.Attempt != 2 {
		t.Errorf("second attempt was told %+v", second)
	}
	// Proof spans baseline to final: both edits in one file.
	if p := r.Proof.Files[0].Patch; !strings.Contains(p, "+// tidy") || !strings.Contains(p, "+  return sum(items);") {
		t.Errorf("proof patch %s", p)
	}
	if r.Proof.Attempt != 2 {
		t.Errorf("proof attempt %d", r.Proof.Attempt)
	}
}

func TestEscalatedAfterThreeRollsBack(t *testing.T) {
	h := newHarness(t, failsWhile("BUG"), func(n int, in agent.FixInput, app string) (agent.Fix, string) {
		return patched("Tidy."), app + "\n"
	}, nil)
	h.start()
	id := h.submit("Total is wrong")
	r := h.wait(id, core.StatusEscalated)
	if len(r.Attempts) != 3 {
		t.Fatalf("attempts %d", len(r.Attempts))
	}
	if h.app() != seed {
		t.Errorf("app.js was not rolled back: %q", h.app())
	}
	if r.Problem == nil || !strings.Contains(r.Problem.Message, "rolled back") || !r.Problem.Retryable {
		t.Errorf("problem %+v", r.Problem)
	}
	if len(h.st.Suite()) != 0 {
		t.Error("an escalated report joined the suite")
	}
}

func TestNotReproducedAskReplyRequeue(t *testing.T) {
	h := newHarness(t, func(_ int, req kane.Request, app string) core.RunStatus {
		if strings.Contains(req.Objective, "menu") || !strings.Contains(app, "BUG") {
			return core.RunPassed
		}
		return core.RunFailed
	}, func(n int, in agent.FixInput, app string) (agent.Fix, string) {
		return patched("Fix."), strings.Replace(app, "BUG", "sum(items)", 1)
	}, nil)
	h.agent.triage = func(in agent.TriageInput) agent.Triage {
		if len(in.Report.Thread) == 0 {
			return agent.Triage{Decision: "check", Title: "Something", Objective: "Go to http://app.test/. Verify the menu shows.", Question: "Which item did you remove?"}
		}
		return agent.Triage{Decision: "check", Title: "Total", Objective: "Go to http://app.test/. Verify the total.", Question: "?"}
	}
	h.start()
	id := h.submit("Something is off with my cart")
	r := h.wait(id, core.StatusNeedsInfo)
	if len(r.Thread) != 1 || r.Thread[0].Text != "Which item did you remove?" {
		t.Fatalf("thread %+v", r.Thread)
	}
	if err := h.w.Reply(id, "Masala Chai, total stayed ₹498"); err != nil {
		t.Fatal(err)
	}
	r = h.wait(id, core.StatusProven)
	if len(r.Thread) != 2 || r.Thread[1].From != "reporter" {
		t.Errorf("thread %+v", r.Thread)
	}
	if err := h.w.Reply(id, "again"); err == nil {
		t.Error("a reply to a proven report was accepted")
	}
}

func TestUnreproducedAfterMaxRounds(t *testing.T) {
	h := newHarness(t, func(int, kane.Request, string) core.RunStatus { return core.RunPassed }, nil, nil)
	h.start()
	id := h.submit("It is broken somehow")
	h.wait(id, core.StatusNeedsInfo)
	h.w.Reply(id, "Still broken")
	h.wait(id, core.StatusNeedsInfo)
	h.w.Reply(id, "Really")
	r := h.wait(id, core.StatusUnreproduced)
	if r.ClosedAt == nil {
		t.Error("not closed")
	}
}

func TestReviseCheck(t *testing.T) {
	h := newHarness(t, func(_ int, req kane.Request, app string) core.RunStatus {
		if strings.Contains(req.Objective, "wrong label") {
			return core.RunFailed
		}
		return failsWhile("BUG")(0, req, app)
	}, func(n int, in agent.FixInput, app string) (agent.Fix, string) {
		if n == 1 {
			return agent.Fix{Action: "revise_check", RootCause: "The label is Remove.", RevisedObjective: "Go to http://app.test/cart. Verify the total.", RevisedExpected: "Right."}, ""
		}
		return patched("Fix."), strings.Replace(app, "BUG", "sum(items)", 1)
	}, nil)
	h.agent.triage = func(agent.TriageInput) agent.Triage {
		return agent.Triage{Decision: "check", Title: "T", Objective: "Go to http://app.test/. Click the wrong label.", Question: "?"}
	}
	h.start()
	id := h.submit("Total is wrong")
	r := h.wait(id, core.StatusProven)
	if r.Check.Revision != 2 || r.Check.StartURL != "http://app.test/cart" {
		t.Errorf("check %+v", r.Check)
	}
	if len(r.Attempts) != 2 || r.Attempts[0].Status != core.AttemptRevisedCheck || r.Proof.Attempt != 1 {
		t.Errorf("attempts %+v proof %+v", r.Attempts, r.Proof)
	}
	reproduces := 0
	for _, run := range r.Runs {
		if run.Kind == core.RunReproduce {
			reproduces++
		}
	}
	if reproduces != 2 {
		t.Errorf("%d reproduce runs, want 2", reproduces)
	}
}

func TestRegressionFeedsNextAttempt(t *testing.T) {
	h := newHarness(t, func(_ int, req kane.Request, app string) core.RunStatus {
		if strings.Contains(req.Objective, "coupon") {
			return failsWhile("BROKE")(0, req, app)
		}
		return failsWhile("BUG")(0, req, app)
	}, func(n int, in agent.FixInput, app string) (agent.Fix, string) {
		if n == 1 {
			return patched("Fix, badly."), strings.Replace(app, "BUG", "sum(items) /* BROKE */", 1)
		}
		return patched("Fix."), strings.Replace(app, " /* BROKE */", "", 1)
	}, nil)
	h.st.AddTest(core.SuiteTest{Title: "Coupon works", Objective: "Go to http://app.test/. Verify the coupon.", ReportID: "r_other", ReportSeq: 9})
	h.start()
	id := h.submit("Total is wrong")
	r := h.wait(id, core.StatusProven)
	if len(r.Attempts) != 2 || r.Attempts[0].Status != core.AttemptRegressed || len(r.Attempts[0].RegressionRunIDs) != 1 {
		t.Fatalf("attempts %+v", r.Attempts)
	}
	if ev := h.agent.fixes[1].Evidence; ev.Kind != core.RunRegression || ev.SuiteTestID == "" {
		t.Errorf("second attempt evidence %+v", ev)
	}
	if r.Proof.RegressionTotal != 1 || r.Proof.RegressionPassed != 1 {
		t.Errorf("proof %+v", r.Proof)
	}
	if tests := h.st.Suite(); len(tests) != 2 || tests[0].LastRun == nil || tests[0].LastRun.Status != core.RunPassed {
		t.Errorf("suite %+v", tests)
	}
}

func TestKaneErrorThenRetry(t *testing.T) {
	h := newHarness(t, func(n int, req kane.Request, app string) core.RunStatus {
		if n == 1 {
			return core.RunErrored
		}
		return failsWhile("BUG")(n, req, app)
	}, func(n int, in agent.FixInput, app string) (agent.Fix, string) {
		return patched("Fix."), strings.Replace(app, "BUG", "sum(items)", 1)
	}, nil)
	h.start()
	id := h.submit("Total is wrong")
	r := h.wait(id, core.StatusError)
	if r.Problem == nil || r.Problem.Stage != "reproduce" || !strings.Contains(r.Problem.Message, "kane-cli whoami") || !r.Problem.Retryable {
		t.Fatalf("problem %+v", r.Problem)
	}
	if err := h.w.Approve(id); err == nil {
		t.Error("approve was accepted for an errored report")
	}
	if err := h.w.Retry(id); err != nil {
		t.Fatal(err)
	}
	r = h.wait(id, core.StatusProven)
	if r.Problem != nil || len(r.Runs) != 2 {
		t.Errorf("after retry: problem %+v, %d runs", r.Problem, len(r.Runs))
	}
}

func TestApprovalWhenAutonomyIsAsk(t *testing.T) {
	h := newHarness(t, failsWhile("BUG"), func(n int, in agent.FixInput, app string) (agent.Fix, string) {
		return patched("Fix."), strings.Replace(app, "BUG", "sum(items)", 1)
	}, func(o *Options) { o.Autonomy = "ask" })
	h.start()
	id := h.submit("Total is wrong")
	h.wait(id, core.StatusAwaitingApproval)
	if len(h.agent.fixes) != 0 {
		t.Fatal("fixed before approval")
	}
	if err := h.w.Approve(id); err != nil {
		t.Fatal(err)
	}
	r := h.wait(id, core.StatusProven)
	reproduces := 0
	for _, run := range r.Runs {
		if run.Kind == core.RunReproduce {
			reproduces++
		}
	}
	if reproduces != 1 {
		t.Errorf("approval re-ran the reproduce step: %d", reproduces)
	}
}

func TestCrashRecovery(t *testing.T) {
	h := newHarness(t, failsWhile("BUG"), func(n int, in agent.FixInput, app string) (agent.Fix, string) {
		return patched("Fix."), strings.Replace(app, "BUG", "sum(items)", 1)
	}, nil)
	// A report caught mid-fix, with a baseline on disk and a half-done patch.
	crashed, _ := h.st.Create(core.Report{Status: core.StatusFixing, Text: "Total is wrong", MaxAttempts: 3,
		Check:    &core.Check{Objective: "Go to http://app.test/. Verify."},
		Attempts: []core.Attempt{{N: 1, Status: core.AttemptRunning}}})
	if err := snapshot.Take(h.dir, filepath.Join(h.data, "snapshots", crashed.ID, "baseline")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(h.dir, "app.js"), []byte("half done"), 0o644)
	waiting, _ := h.st.Create(core.Report{Status: core.StatusQueued, Text: "Another total problem", MaxAttempts: 3})
	h.start()
	r := h.wait(crashed.ID, core.StatusError)
	if r.Problem == nil || r.Problem.Message != "Fixpoint was restarted while this was in progress." || r.Problem.Stage != "fix" {
		t.Errorf("problem %+v", r.Problem)
	}
	if r.Attempts[0].Status == core.AttemptRunning {
		t.Error("the interrupted attempt is still running")
	}
	h.wait(waiting.ID, core.StatusProven)
	if err := h.w.Retry(crashed.ID); err != nil {
		t.Fatal(err)
	}
	h.wait(crashed.ID, core.StatusProven)
}

func TestRollbackOnRestartedCrashRestoresFiles(t *testing.T) {
	h := newHarness(t, failsWhile("BUG"), nil, nil)
	crashed, _ := h.st.Create(core.Report{Status: core.StatusVerifying, Text: "x", MaxAttempts: 3})
	snapshot.Take(h.dir, filepath.Join(h.data, "snapshots", crashed.ID, "baseline"))
	os.WriteFile(filepath.Join(h.dir, "app.js"), []byte("patched"), 0o644)
	os.WriteFile(filepath.Join(h.dir, "added.js"), []byte("new\n"), 0o644)
	h.start()
	h.wait(crashed.ID, core.StatusError)
	if h.app() != seed {
		t.Errorf("app.js = %q", h.app())
	}
	if _, err := os.Stat(filepath.Join(h.dir, "added.js")); err == nil {
		t.Error("added file kept after rollback")
	}
}

func TestShutdownMidRunMarksError(t *testing.T) {
	block := make(chan struct{})
	h := newHarness(t, func(int, kane.Request, string) core.RunStatus { <-block; return core.RunFailed }, nil, nil)
	h.runner.decide = func(int, kane.Request, string) core.RunStatus {
		<-block
		return core.RunFailed
	}
	h.start()
	id := h.submit("Total is wrong")
	h.waitStatus(id, core.StatusReproducing)
	go func() { time.Sleep(50 * time.Millisecond); close(block) }()
	h.stop()
	r, _ := h.st.Get(id)
	if r.Status != core.StatusError || r.Problem == nil || !strings.Contains(r.Problem.Message, "stopped") {
		t.Errorf("after shutdown: %s %+v", r.Status, r.Problem)
	}
}

func TestPositionAndStartURL(t *testing.T) {
	h := newHarness(t, failsWhile("BUG"), nil, nil)
	a := h.submit("first report here")
	b := h.submit("second report here")
	if h.w.Position(a) != 1 || h.w.Position(b) != 2 || h.w.Pending() != 2 {
		t.Errorf("positions %d %d", h.w.Position(a), h.w.Position(b))
	}
	for in, want := range map[string]string{
		"Go to http://localhost:4000. Click.":   "http://localhost:4000",
		"go to https://x.test/a?b=1, then":      "https://x.test/a?b=1",
		"Open the page and click.":              "http://fallback",
		"Go to http://localhost:4000/#/cart.\n": "http://localhost:4000/#/cart",
	} {
		if got := startURL(in, "http://fallback"); got != want {
			t.Errorf("startURL(%q) = %q, want %q", in, got, want)
		}
	}
}
