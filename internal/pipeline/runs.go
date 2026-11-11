package pipeline

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
	"github.com/Manas-Sehgxl/fixpoint/internal/kane"
	"github.com/Manas-Sehgxl/fixpoint/internal/view"
)

type runSpec struct {
	kind        core.RunKind
	stage       string
	attempt     int
	title       string
	objective   string
	startURL    string
	name        string
	testFile    string
	suiteTestID string
}

// kaneRun runs one check for a report, recording the run on the report as
// it starts, as steps arrive and when it ends.
func (w *Worker) kaneRun(ctx context.Context, id string, s runSpec) kane.Result {
	runID, err := w.o.Store.NextRunID()
	if err != nil {
		runID = fmt.Sprintf("run_x%d", len(s.objective))
	}
	run := core.Run{ID: runID, Kind: s.kind, Attempt: s.attempt, SuiteTestID: s.suiteTestID, Title: s.title,
		Objective: s.objective, Status: core.RunRunning, Simulated: w.o.Simulated, Steps: []core.Step{}, FinalState: map[string]any{}}
	r, _ := w.update(id, func(r *core.Report) {
		run.StartedAt = r.UpdatedAt
		r.Runs = append(r.Runs, run)
	})
	w.emit(id, core.EvRunStarted, s.stage, startText(s, r.Seq), map[string]any{"run_id": runID, "kind": string(s.kind), "attempt": s.attempt, "title": s.title})

	var mu sync.Mutex
	var sent []core.Step
	onStep := func(steps []core.Step) {
		mu.Lock()
		defer mu.Unlock()
		w.update(id, func(r *core.Report) {
			for i := range r.Runs {
				if r.Runs[i].ID == runID {
					r.Runs[i].Steps = steps
				}
			}
		})
		// Announce every step that is new or changed since the last call.
		for i, st := range steps {
			if i < len(sent) && sent[i] == st {
				continue
			}
			w.emit(id, core.EvRunStep, s.stage, st.Remark, map[string]any{"run_id": runID, "n": st.N, "status": st.Status, "remark": st.Remark})
		}
		sent = steps
	}
	dir := filepath.Join(w.o.ArtifactsDir, id, runID)
	res := w.o.Runner.Run(ctx, kane.Request{Objective: s.objective, StartURL: s.startURL, Name: s.name, TestFile: s.testFile,
		ArtifactDir: dir, ArtifactURL: "/artifacts/" + id + "/" + runID}, onStep)
	mu.Lock()
	defer mu.Unlock()
	out := res.Run
	out.ID, out.Kind, out.Attempt, out.SuiteTestID, out.Title, out.Objective = runID, s.kind, s.attempt, s.suiteTestID, s.title, s.objective
	out.Simulated = w.o.Simulated
	out.StartedAt = run.StartedAt
	view.NormalizeRun(&out)
	w.update(id, func(r *core.Report) {
		for i := range r.Runs {
			if r.Runs[i].ID == runID {
				r.Runs[i] = out
			}
		}
	})
	res.Run = out
	w.emit(id, core.EvRunFinished, s.stage, finishText(s, out), map[string]any{"run_id": runID, "kind": string(s.kind), "status": string(out.Status), "reason": out.Reason})
	return res
}

func startText(s runSpec, seq int) string {
	switch s.kind {
	case core.RunReproduce:
		return "Kane CLI started the check to reproduce the problem."
	case core.RunVerify:
		return "Kane CLI started the check again on the patched app."
	case core.RunRegression:
		return fmt.Sprintf("Kane CLI is re-checking an earlier fix: %s.", strings.TrimRight(s.title, "."))
	}
	return "Kane CLI started a run."
}

func finishText(s runSpec, run core.Run) string {
	what := strings.TrimRight(describe(run), ".")
	switch run.Status {
	case core.RunErrored, core.RunTimeout:
		return "Kane CLI could not finish the run: " + strings.TrimRight(run.Error, ".") + "."
	}
	switch s.kind {
	case core.RunReproduce:
		if run.Status == core.RunFailed {
			return "Kane reproduced it: " + what + "."
		}
		return "Kane ran the check and it passed, so the problem did not show up."
	case core.RunVerify:
		if run.Status == core.RunFailed {
			return "Kane still sees the problem: " + what + "."
		}
		return "Kane ran the same check on the patched app and it passed."
	case core.RunRegression:
		if run.Status == core.RunFailed {
			return fmt.Sprintf("An earlier fix broke (%s): %s.", strings.TrimRight(s.title, "."), what)
		}
		return fmt.Sprintf("An earlier fix still holds: %s.", strings.TrimRight(s.title, "."))
	}
	return "Kane CLI finished: " + string(run.Status) + "."
}

// runSuite re-runs every suite test, replaying saved tests when they exist.
func (w *Worker) runSuite(ctx context.Context) {
	tests := w.o.Store.Suite()
	w.emit("", core.EvSuiteStarted, "", fmt.Sprintf("Re-running all %d checks in the suite.", len(tests)), map[string]any{"total": len(tests)})
	passed := 0
	for _, t := range tests {
		if ctx.Err() != nil {
			return
		}
		runID, _ := w.o.Store.NextRunID()
		w.emit("", core.EvSuiteTest, "", "Running: "+t.Title, map[string]any{"test_id": t.ID, "status": string(core.RunRunning)})
		res := w.o.Runner.Run(ctx, kane.Request{Objective: t.Objective, StartURL: t.StartURL, TestFile: existing(t.TestFile),
			ArtifactDir: filepath.Join(w.o.ArtifactsDir, "suite", runID), ArtifactURL: "/artifacts/suite/" + runID}, nil)
		if ctx.Err() != nil {
			return
		}
		w.recordSuite(t.ID, res)
		if res.Run.Status == core.RunPassed {
			passed++
		}
		w.emit("", core.EvSuiteTest, "", fmt.Sprintf("%s: %s.", t.Title, res.Run.Status), map[string]any{"test_id": t.ID, "status": string(res.Run.Status)})
	}
	w.emit("", core.EvSuiteFinished, "", fmt.Sprintf("%d of %d checks passed.", passed, len(tests)), map[string]any{"total": len(tests), "passed": passed})
}
