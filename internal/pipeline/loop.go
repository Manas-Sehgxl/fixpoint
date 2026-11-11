package pipeline

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/agent"
	"github.com/Manas-Sehgxl/fixpoint/internal/core"
	"github.com/Manas-Sehgxl/fixpoint/internal/kane"
	"github.com/Manas-Sehgxl/fixpoint/internal/proc"
	"github.com/Manas-Sehgxl/fixpoint/internal/snapshot"
	"github.com/Manas-Sehgxl/fixpoint/internal/store"
	"github.com/Manas-Sehgxl/fixpoint/internal/view"
)

type stage int

const (
	stTriage stage = iota
	stReproduce
	stFix
	stDone
)

const defaultQuestion = "What were you doing when it went wrong, and what did you expect to happen?"

func (w *Worker) processReport(ctx context.Context, j job) {
	r, err := w.o.Store.Get(j.id)
	if err != nil || r.Status != core.StatusQueued {
		return
	}
	st := stTriage
	if j.resume && r.Check != nil && lastFailing(r) != nil {
		st = stFix
	} else {
		// A fresh start: put back anything an earlier pass left behind.
		if w.restoreBaseline(ctx, j.id) {
			w.dropSnapshots(j.id)
		}
		if r.Check != nil || r.Problem != nil {
			// Start triage from a clean slate, but keep the runs and
			// attempts already in the case file.
			w.update(j.id, requeueWithThread)
		}
	}
	for st != stDone && ctx.Err() == nil {
		switch st {
		case stTriage:
			st = w.triage(ctx, j.id)
		case stReproduce:
			st = w.reproduce(ctx, j.id)
		case stFix:
			st = w.fixLoop(ctx, j.id)
		}
	}
}

var goTo = regexp.MustCompile(`(?i)^\s*go to\s+(https?://\S+?)[.,;]?(\s|$)`)

// startURL takes the URL from the check's opening "Go to <URL>." step.
func startURL(objective, fallback string) string {
	if m := goTo.FindStringSubmatch(objective); m != nil {
		return m[1]
	}
	return fallback
}

func (w *Worker) triage(ctx context.Context, id string) stage {
	r := w.setStatus(id, core.StatusTriaging, "triage", w.o.Agent.Name()+" is reading the report to write a check.")
	t, call, err := w.o.Agent.Triage(ctx, agent.TriageInput{TargetURL: w.o.Target.URL, TargetNotes: w.o.Target.Notes, Report: r})
	if ctx.Err() != nil {
		return stDone
	}
	if err != nil {
		w.fail(ctx, id, "triage", "The coding agent could not write a check: "+err.Error(),
			"Run `fixpoint doctor` to check the agent, then retry this report.")
		return stDone
	}
	if t.Decision == "ask" {
		if strings.TrimSpace(t.Title) != "" {
			w.update(id, func(r *core.Report) { r.Title = view.Clip(t.Title, 70) })
		}
		w.ask(id, t.Question)
		return stDone
	}
	w.mu.Lock()
	w.followUp[id] = t.Question
	w.mu.Unlock()
	check := core.Check{Title: view.Clip(t.Title, 70), Objective: strings.TrimSpace(t.Objective), Expected: t.Expected,
		StartURL: startURL(t.Objective, w.o.Target.URL), WrittenBy: w.o.Agent.Name(), Revision: 1}
	if check.Title == "" {
		check.Title = r.Title
	}
	w.update(id, func(r *core.Report) {
		r.Check = &check
		r.Title = check.Title
	})
	w.emit(id, core.EvCheckWritten, "triage", fmt.Sprintf("%s wrote a check: %s.", w.o.Agent.Name(), strings.TrimRight(check.Title, ".")),
		map[string]any{"revision": 1, "duration_ms": call.DurationMS, "cost_usd": call.CostUSD})
	return stReproduce
}

// ask puts a question to the reporter, or closes the report when the
// question rounds are used up.
func (w *Worker) ask(id, question string) {
	if strings.TrimSpace(question) == "" {
		question = defaultQuestion
	}
	r, err := w.o.Store.Get(id)
	if err != nil {
		return
	}
	asked := 0
	for _, m := range r.Thread {
		if m.From == "fixpoint" {
			asked++
		}
	}
	if asked >= w.o.MaxInfoRounds {
		now := time.Now().UTC()
		w.update(id, func(r *core.Report) { r.Status = core.StatusUnreproduced; r.ClosedAt = &now })
		w.dropSnapshots(id)
		w.emit(id, core.EvReportStatus, "reproduce",
			fmt.Sprintf("Closed as not reproduced: Kane could not make it happen after %d questions to the reporter.", asked),
			map[string]any{"status": string(core.StatusUnreproduced)})
		return
	}
	w.update(id, func(r *core.Report) {
		r.Status = core.StatusNeedsInfo
		r.Thread = append(r.Thread, core.Message{From: "fixpoint", Text: question, At: time.Now().UTC()})
	})
	w.emit(id, core.EvReportStatus, "reproduce", "Waiting for the reporter to answer a question.", map[string]any{"status": string(core.StatusNeedsInfo)})
	w.emit(id, core.EvReportQuestion, "reproduce", "Fixpoint asked the reporter: "+question, map[string]any{"question": question})
}

func (w *Worker) reproduce(ctx context.Context, id string) stage {
	r := w.setStatus(id, core.StatusReproducing, "reproduce", "Kane CLI is running the check in a real browser.")
	if r.Check == nil {
		return stTriage
	}
	res := w.kaneRun(ctx, id, runSpec{kind: core.RunReproduce, stage: "reproduce", title: r.Check.Title,
		objective: r.Check.Objective, startURL: r.Check.StartURL, name: fmt.Sprintf("fp-%d-r%d", r.Seq, r.Check.Revision)})
	if ctx.Err() != nil {
		return stDone
	}
	switch res.Run.Status {
	case core.RunPassed:
		w.mu.Lock()
		q := w.followUp[id]
		w.mu.Unlock()
		w.ask(id, q)
		return stDone
	case core.RunFailed:
		if res.TestFile != "" {
			w.update(id, func(r *core.Report) { r.Check.TestFile = res.TestFile })
		}
		if w.o.Autonomy == "ask" && len(r.Attempts) == 0 {
			w.setStatus(id, core.StatusAwaitingApproval, "fix", "Reproduced. Waiting for a person to approve a fix.")
			return stDone
		}
		return stFix
	default:
		w.fail(ctx, id, "reproduce", res.Run.Error, "")
		return stDone
	}
}

// lastFailing is the most recent failed run: the evidence the next attempt
// answers.
func lastFailing(r core.Report) *core.Run {
	for i := len(r.Runs) - 1; i >= 0; i-- {
		if r.Runs[i].Status == core.RunFailed {
			return &r.Runs[i]
		}
	}
	return nil
}

func anyPatched(r core.Report) bool {
	for _, a := range r.Attempts {
		if len(a.Files) > 0 {
			return true
		}
	}
	return false
}

func (w *Worker) finishAttempt(id string, n int, status core.AttemptStatus, note string, fn func(*core.Attempt)) {
	now := time.Now().UTC()
	w.update(id, func(r *core.Report) {
		for i := range r.Attempts {
			if r.Attempts[i].N == n {
				a := &r.Attempts[i]
				a.Status, a.Note, a.EndedAt = status, note, &now
				if fn != nil {
					fn(a)
				}
			}
		}
	})
	text := fmt.Sprintf("Attempt %d ended: %s", n, strings.ReplaceAll(string(status), "_", " "))
	if note != "" {
		text = fmt.Sprintf("Attempt %d did not hold: %s", n, note)
	}
	if status == core.AttemptVerified {
		text = fmt.Sprintf("Attempt %d held: the check passes and nothing else broke.", n)
	}
	w.emit(id, core.EvAttemptFinished, "fix", text, map[string]any{"n": n, "status": string(status)})
}

func (w *Worker) fixLoop(ctx context.Context, id string) stage {
	for ctx.Err() == nil {
		r, err := w.o.Store.Get(id)
		if err != nil {
			return stDone
		}
		if view.CountedAttempts(r) >= w.o.MaxAttempts {
			w.escalate(ctx, id, fmt.Sprintf("%d fix attempts did not make the check pass, so every patch was rolled back.", view.CountedAttempts(r)))
			return stDone
		}
		evidence := lastFailing(r)
		if evidence == nil || r.Check == nil {
			w.fail(ctx, id, "fix", "There is no failing run to fix against.", "Retry this report.")
			return stDone
		}
		n := len(r.Attempts) + 1
		counted := view.CountedAttempts(r) + 1
		base := w.snapDir(id, "baseline")
		if !snapshot.Exists(base) {
			if err := snapshot.Take(w.o.Target.Dir, base, w.o.Exclude...); err != nil {
				w.fail(ctx, id, "fix", "Could not snapshot the app before fixing: "+err.Error(), "Check that target.dir exists and is readable.")
				return stDone
			}
		}
		pre := w.snapDir(id, fmt.Sprintf("attempt-%d", n))
		if err := snapshot.Take(w.o.Target.Dir, pre, w.o.Exclude...); err != nil {
			w.fail(ctx, id, "fix", "Could not snapshot the app before the attempt: "+err.Error(), "Check that target.dir is readable.")
			return stDone
		}
		w.update(id, func(r *core.Report) {
			r.Status = core.StatusFixing
			r.Attempts = append(r.Attempts, core.Attempt{N: n, Status: core.AttemptRunning, Files: []core.FileDiff{},
				RegressionRunIDs: []string{}, StartedAt: time.Now().UTC(), Agent: core.AgentCall{Name: w.o.Agent.Name()}})
		})
		w.emit(id, core.EvReportStatus, "fix", fmt.Sprintf("%s is working on fix attempt %d of %d.", w.o.Agent.Name(), counted, w.o.MaxAttempts),
			map[string]any{"status": string(core.StatusFixing)})
		w.emit(id, core.EvAttemptStarted, "fix", fmt.Sprintf("Attempt %d started.", counted), map[string]any{"n": n})

		fix, call, err := w.o.Agent.Fix(ctx, agent.FixInput{TargetURL: w.o.Target.URL, ReportText: r.Text, Check: *r.Check,
			Attempt: counted, MaxAttempts: w.o.MaxAttempts, Evidence: *evidence, Previous: r.Attempts})
		if ctx.Err() != nil {
			return stDone
		}
		w.update(id, func(r *core.Report) {
			a := &r.Attempts[len(r.Attempts)-1]
			a.Agent = call
			a.RootCause, a.Summary = fix.RootCause, fix.Summary
		})
		if err != nil {
			_ = snapshot.Restore(pre, w.o.Target.Dir, w.o.Exclude...)
			w.finishAttempt(id, n, core.AttemptAgentError, "The coding agent failed: "+err.Error(), nil)
			w.fail(ctx, id, "fix", "The coding agent failed during attempt "+fmt.Sprint(counted)+": "+err.Error(),
				"Run `fixpoint doctor` to check the agent, then retry this report.")
			return stDone
		}
		switch fix.Action {
		case "give_up":
			_ = snapshot.Restore(pre, w.o.Target.Dir, w.o.Exclude...)
			w.finishAttempt(id, n, core.AttemptGaveUp, "The coding agent gave up: "+view.Clip(fix.RootCause, 200), nil)
			w.escalate(ctx, id, "The coding agent could not find a fix, so every patch was rolled back.")
			return stDone
		case "revise_check":
			_ = snapshot.Restore(pre, w.o.Target.Dir, w.o.Exclude...)
			if r.Check.Revision >= 2 || anyPatched(r) || strings.TrimSpace(fix.RevisedObjective) == "" {
				w.finishAttempt(id, n, core.AttemptNoChange,
					"The agent asked to change the check, which is allowed once and only before any patch has landed.", nil)
				continue
			}
			w.finishAttempt(id, n, core.AttemptRevisedCheck, "", nil)
			nc := *r.Check
			nc.Objective = strings.TrimSpace(fix.RevisedObjective)
			if fix.RevisedExpected != "" {
				nc.Expected = fix.RevisedExpected
			}
			nc.StartURL = startURL(nc.Objective, w.o.Target.URL)
			nc.Revision = 2
			nc.TestFile = ""
			w.update(id, func(r *core.Report) { r.Check = &nc })
			w.emit(id, core.EvCheckWritten, "fix", w.o.Agent.Name()+" found the check was wrong and corrected it; Kane runs the new check.",
				map[string]any{"revision": 2})
			return stReproduce
		}

		files, err := snapshot.Diff(pre, w.o.Target.Dir, w.o.Exclude...)
		if err != nil {
			w.fail(ctx, id, "fix", "Could not compare the app with its snapshot: "+err.Error(), "Retry this report.")
			return stDone
		}
		if len(files) == 0 {
			w.finishAttempt(id, n, core.AttemptNoChange, "The agent said it patched the app, but no file changed.", nil)
			continue
		}
		adds, dels := 0, 0
		paths := make([]string, 0, len(files))
		for _, f := range files {
			adds += f.Additions
			dels += f.Deletions
			paths = append(paths, f.Path)
		}
		w.update(id, func(r *core.Report) { r.Attempts[len(r.Attempts)-1].Files = files })
		w.emit(id, core.EvAttemptPatched, "fix", fmt.Sprintf("Attempt %d patched %s (+%d -%d): %s", counted, strings.Join(paths, ", "), adds, dels, strings.TrimSpace(fix.Summary)),
			map[string]any{"n": n, "files": paths, "additions": adds, "deletions": dels})
		if err := w.restart(ctx); err != nil {
			w.finishAttempt(id, n, core.AttemptStillFailing, "The app did not come back after the patch: "+err.Error(), nil)
			w.fail(ctx, id, "verify", "The app did not come back after the patch: "+err.Error(), "Check target.restart_cmd and target.health_url.")
			return stDone
		}

		r = w.setStatus(id, core.StatusVerifying, "verify", "Kane CLI is running the same check against the patched app.")
		res := w.kaneRun(ctx, id, runSpec{kind: core.RunVerify, stage: "verify", attempt: n, title: r.Check.Title,
			objective: r.Check.Objective, startURL: r.Check.StartURL, name: fmt.Sprintf("fp-%d-v%d", r.Seq, n)})
		if ctx.Err() != nil {
			return stDone
		}
		w.update(id, func(r *core.Report) { r.Attempts[len(r.Attempts)-1].VerifyRunID = res.Run.ID })
		switch res.Run.Status {
		case core.RunFailed:
			w.finishAttempt(id, n, core.AttemptStillFailing, "Kane still sees the problem: "+describe(res.Run), nil)
			continue
		case core.RunPassed:
		default:
			w.finishAttempt(id, n, core.AttemptStillFailing, "Kane could not verify the patch: "+res.Run.Error, nil)
			w.fail(ctx, id, "verify", res.Run.Error, "")
			return stDone
		}

		broken, ok := w.regress(ctx, id, n)
		if ctx.Err() != nil || !ok {
			return stDone
		}
		if broken != nil {
			w.finishAttempt(id, n, core.AttemptRegressed, fmt.Sprintf("It broke an earlier fix (%s): %s", broken.Title, describe(*broken)), nil)
			continue
		}
		testFile := res.TestFile
		w.finishAttempt(id, n, core.AttemptVerified, "", nil)
		w.prove(ctx, id, res.Run, n, testFile)
		return stDone
	}
	return stDone
}

// describe is one short phrase for what a run saw.
func describe(run core.Run) string {
	switch {
	case run.Summary != "":
		return run.Summary
	case run.Reason != "":
		return run.Reason
	case run.Error != "":
		return run.Error
	}
	return "the check did not pass."
}

// regress re-runs every suite test from earlier reports. It returns the
// first failing run, or ok false when the report stopped with an error.
func (w *Worker) regress(ctx context.Context, id string, n int) (*core.Run, bool) {
	var tests []core.SuiteTest
	for _, t := range w.o.Store.Suite() {
		if t.ReportID != id {
			tests = append(tests, t)
		}
	}
	text := "No earlier fixes to re-check."
	if len(tests) > 0 {
		text = fmt.Sprintf("Kane CLI is re-running the checks of %d earlier fixes.", len(tests))
	}
	w.setStatus(id, core.StatusRegressing, "regress", text)
	for _, t := range tests {
		res := w.kaneRun(ctx, id, runSpec{kind: core.RunRegression, stage: "regress", attempt: n, title: t.Title,
			objective: t.Objective, startURL: t.StartURL, testFile: existing(t.TestFile), suiteTestID: t.ID})
		if ctx.Err() != nil {
			return nil, false
		}
		w.update(id, func(r *core.Report) {
			a := &r.Attempts[len(r.Attempts)-1]
			a.RegressionRunIDs = append(a.RegressionRunIDs, res.Run.ID)
		})
		w.recordSuite(t.ID, res)
		switch res.Run.Status {
		case core.RunPassed:
		case core.RunFailed:
			run := res.Run
			return &run, true
		default:
			w.finishAttempt(id, n, core.AttemptStillFailing, "Kane could not re-run an earlier fix's check: "+res.Run.Error, nil)
			w.fail(ctx, id, "regress", res.Run.Error, "")
			return nil, false
		}
	}
	return nil, true
}

func existing(p string) string {
	if p == "" {
		return ""
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

func (w *Worker) recordSuite(testID string, res kane.Result) {
	end := time.Now().UTC()
	if res.Run.EndedAt != nil {
		end = *res.Run.EndedAt
	}
	_ = w.o.Store.SetTestResult(testID, core.SuiteResult{Status: res.Run.Status, Reason: firstNonEmpty(res.Run.Reason, res.Run.Error),
		DurationS: res.Run.DurationS, Replayed: res.Replayed, At: end})
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func (w *Worker) prove(ctx context.Context, id string, green core.Run, n int, testFile string) {
	r, err := w.o.Store.Get(id)
	if err != nil || r.Check == nil {
		return
	}
	files, err := snapshot.Diff(w.snapDir(id, "baseline"), w.o.Target.Dir, w.o.Exclude...)
	if err != nil {
		files = []core.FileDiff{}
	}
	patchURL := ""
	patchPath := filepath.Join(w.o.ArtifactsDir, id, "fix.patch")
	if err := store.WriteFileAtomic(patchPath, []byte(snapshot.JoinPatches(files))); err == nil {
		patchURL = "/api/reports/" + id + "/patch"
	}
	if testFile == "" {
		testFile = r.Check.TestFile
	}
	t, err := w.o.Store.AddTest(core.SuiteTest{Title: r.Check.Title, Objective: r.Check.Objective, StartURL: r.Check.StartURL,
		TestFile: testFile, ReportID: id, ReportSeq: r.Seq})
	if err == nil {
		w.recordSuite(t.ID, kane.Result{Run: green})
	}
	red := ""
	for _, run := range r.Runs {
		if run.Kind == core.RunReproduce && run.Status == core.RunFailed {
			red = run.ID
		}
	}
	regTotal, regPassed := 0, 0
	for _, a := range r.Attempts {
		if a.N == n {
			for _, rid := range a.RegressionRunIDs {
				regTotal++
				for _, run := range r.Runs {
					if run.ID == rid && run.Status == core.RunPassed {
						regPassed++
					}
				}
			}
		}
	}
	now := time.Now().UTC()
	counted := 0
	for _, a := range r.Attempts {
		if a.Status != core.AttemptRevisedCheck {
			counted++
		}
		if a.N == n {
			break
		}
	}
	w.update(id, func(r *core.Report) {
		r.Proof = &core.Proof{RedRunID: red, GreenRunID: green.ID, Attempt: counted, TimeToProofS: now.Sub(r.CreatedAt).Seconds(),
			Files: files, PatchURL: patchURL, SuiteTestID: t.ID, RegressionTotal: regTotal, RegressionPassed: regPassed}
		r.Proof.BeforeShot, r.Proof.AfterShot = view.Shots(*r)
		r.Status = core.StatusProven
		r.ClosedAt = &now
	})
	w.dropSnapshots(id)
	w.emit(id, core.EvReportStatus, "verify", "Proven: Kane saw it fail, the patch landed and Kane saw it pass.", map[string]any{"status": string(core.StatusProven)})
	w.emit(id, core.EvReportProven, "verify", fmt.Sprintf("Fixed and verified on attempt %d. The check joined the regression suite as test %s.", counted, strings.TrimPrefix(t.ID, "t_")),
		map[string]any{"attempt": counted, "suite_test_id": t.ID})
}

func (w *Worker) escalate(ctx context.Context, id, msg string) {
	rolled := w.restoreBaseline(ctx, id)
	r, err := w.o.Store.Get(id)
	if err != nil {
		return
	}
	stage := "fix"
	first := ""
	for i := len(r.Attempts) - 1; i >= 0; i-- {
		a := r.Attempts[i]
		if a.Status == core.AttemptStillFailing || a.Status == core.AttemptRegressed {
			stage = "verify"
		}
		if first == "" && a.RootCause != "" {
			first = fmt.Sprintf("Look first at what attempt %d found: %s", a.N, a.RootCause)
		}
	}
	if !rolled {
		msg = strings.Replace(msg, ", so every patch was rolled back", "", 1)
	}
	if first == "" {
		first = "Start from the failing run's steps and screenshot."
	}
	now := time.Now().UTC()
	w.update(id, func(r *core.Report) {
		r.Status = core.StatusEscalated
		r.Problem = &core.Problem{Stage: stage, Message: msg, Remedy: first, Retryable: true}
		r.ClosedAt = &now
	})
	w.dropSnapshots(id)
	w.emit(id, core.EvReportStatus, stage, "Handed to a person.", map[string]any{"status": string(core.StatusEscalated)})
	w.emit(id, core.EvReportEscalated, stage, msg, nil)
}

func (w *Worker) fail(ctx context.Context, id, stage, msg, remedy string) {
	if msg == "" {
		msg = "Something went wrong."
	}
	if remedy == "" {
		remedy = "Fix the problem above, then retry this report."
	}
	if w.restoreBaseline(ctx, id) {
		remedy = "The app's files were put back as they were before the first attempt. " + remedy
	}
	w.update(id, func(r *core.Report) {
		r.Status = core.StatusError
		r.Problem = &core.Problem{Stage: stage, Message: msg, Remedy: remedy, Retryable: true}
	})
	w.emit(id, core.EvReportStatus, stage, "Stopped by a problem: "+msg, map[string]any{"status": string(core.StatusError)})
	w.emit(id, core.EvReportError, stage, msg, nil)
}

// restart runs the configured restart command and waits for the app.
func (w *Worker) restart(ctx context.Context) error {
	if w.o.Restart != nil {
		return w.o.Restart(ctx)
	}
	if w.o.Target.RestartCmd == "" {
		return nil
	}
	rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	bin, args := proc.Shell(w.o.Target.RestartCmd)
	var out strings.Builder
	code, err := proc.Run(rctx, proc.Spec{Bin: bin, Args: args, Dir: w.o.Target.Dir, Stdout: &out, Stderr: &out})
	if err != nil {
		return fmt.Errorf("restart command failed to run: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("restart command exited with code %d: %s", code, view.Clip(out.String(), 200))
	}
	url := firstNonEmpty(w.o.Target.HealthURL, w.o.Target.URL)
	return WaitHealthy(rctx, url, 60*time.Second)
}

// WaitHealthy polls url until it answers with a status below 500.
func WaitHealthy(ctx context.Context, url string, limit time.Duration) error {
	if url == "" {
		return nil
	}
	deadline := time.Now().Add(limit)
	client := &http.Client{Timeout: 3 * time.Second}
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not answer within %s", url, limit)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
