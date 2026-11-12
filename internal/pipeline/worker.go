// Package pipeline is the single worker that takes reports through the loop
// of spec section 3: triage, reproduce, fix, verify, regress, prove. One
// report is worked on at a time because fixes change one working tree.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/agent"
	"github.com/Manas-Sehgxl/fixpoint/internal/core"
	"github.com/Manas-Sehgxl/fixpoint/internal/events"
	"github.com/Manas-Sehgxl/fixpoint/internal/kane"
	"github.com/Manas-Sehgxl/fixpoint/internal/snapshot"
	"github.com/Manas-Sehgxl/fixpoint/internal/store"
	"github.com/Manas-Sehgxl/fixpoint/internal/view"
)

// Runner runs one Kane check. kane.CLI implements it; tests use fakes.
type Runner interface {
	Run(ctx context.Context, req kane.Request, onStep func([]core.Step)) kane.Result
}

// Target is the app under repair.
type Target struct {
	Name       string
	URL        string
	Dir        string
	Notes      string
	RestartCmd string
	HealthURL  string
}

// Options configures a Worker.
type Options struct {
	Store         *store.Store
	Bus           *events.Bus
	Runner        Runner
	Agent         agent.Agent
	Target        Target
	MaxAttempts   int
	MaxInfoRounds int
	Autonomy      string // auto | ask
	ArtifactsDir  string
	SnapshotsDir  string
	Exclude       []string // directories inside the target that snapshots skip
	Simulated     bool
	// Restart runs after every patch and rollback; nil uses Target.RestartCmd.
	Restart func(ctx context.Context) error
}

// Conflict is returned when an action does not apply to a report's status.
type Conflict struct{ Msg string }

func (c *Conflict) Error() string { return c.Msg }

type job struct {
	id     string // report id; empty for a suite run
	resume bool   // continue at fixing (after approval)
}

type current struct {
	job    job
	cancel context.CancelFunc
	done   chan struct{}
}

// Worker owns the queue and the target's working tree.
type Worker struct {
	o Options

	mu       sync.Mutex
	queue    []job
	cur      *current
	paused   bool
	suiteRun bool
	followUp map[string]string // the question triage would ask if the check passes
	wake     chan struct{}
}

// New returns a worker; call Recover, then Run.
func New(o Options) *Worker {
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 3
	}
	if o.MaxInfoRounds <= 0 {
		o.MaxInfoRounds = 2
	}
	if o.Autonomy == "" {
		o.Autonomy = "auto"
	}
	return &Worker{o: o, followUp: map[string]string{}, wake: make(chan struct{}, 1)}
}

func (w *Worker) poke() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Enqueue adds a report to the end of the queue unless it is already there.
func (w *Worker) enqueue(j job) {
	w.mu.Lock()
	for _, q := range w.queue {
		if q.id == j.id && j.id != "" {
			w.mu.Unlock()
			return
		}
	}
	w.queue = append(w.queue, j)
	w.mu.Unlock()
	w.poke()
}

// Position returns a queued report's place in line (1 = next), or 0.
func (w *Worker) Position(id string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, q := range w.queue {
		if q.id == "" {
			continue
		}
		n++
		if q.id == id {
			return n
		}
	}
	return 0
}

// Queue describes what the worker is doing.
func (w *Worker) Queue() core.QueueInfo {
	w.mu.Lock()
	defer w.mu.Unlock()
	q := core.QueueInfo{Busy: w.cur != nil}
	if w.cur != nil {
		q.ActiveReportID = w.cur.job.id
	}
	for _, j := range w.queue {
		if j.id != "" {
			q.Pending++
		}
	}
	return q
}

// Pending is the number of reports waiting in the queue.
func (w *Worker) Pending() int { return w.Queue().Pending }

// SuiteRunning reports whether a suite run is queued or in progress.
func (w *Worker) SuiteRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.suiteRun
}

// Fill sets a report's derived fields using the live queue.
func (w *Worker) Fill(r *core.Report) { view.Fill(r, w.Position(r.ID)) }

func (w *Worker) emit(id, typ, stage, text string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	w.o.Bus.Publish(core.Event{ReportID: id, Type: typ, Stage: stage, Text: text, Data: data})
}

func (w *Worker) update(id string, fn func(*core.Report)) (core.Report, error) {
	r, err := w.o.Store.Update(id, func(r *core.Report) error { fn(r); return nil })
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		log.Printf("fixpoint: could not save report %s: %v", id, err)
	}
	return r, err
}

func (w *Worker) setStatus(id string, s core.Status, stage, text string) core.Report {
	r, _ := w.update(id, func(r *core.Report) { r.Status = s })
	w.emit(id, core.EvReportStatus, stage, text, map[string]any{"status": string(s)})
	return r
}

// Submit creates a report and queues it.
func (w *Worker) Submit(req core.NewReportRequest) (core.Report, error) {
	text := strings.TrimSpace(req.Text)
	r, err := w.o.Store.Create(core.Report{
		Status: core.StatusQueued, Title: view.Clip(firstLine(text), 70), Text: text,
		Reporter: req.Reporter, Context: req.Context, Thread: []core.Message{},
		Runs: []core.Run{}, Attempts: []core.Attempt{}, MaxAttempts: w.o.MaxAttempts, Simulated: w.o.Simulated,
	})
	if err != nil {
		return r, err
	}
	w.emit(r.ID, core.EvReportCreated, "", fmt.Sprintf("Report #%d arrived: %s", r.Seq, view.Clip(text, 120)), nil)
	w.enqueue(job{id: r.ID})
	return r, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// resetWork clears what the loop produced so a report can start again.
func resetWork(r *core.Report) {
	r.Status = core.StatusQueued
	r.Check, r.Proof, r.Problem, r.ClosedAt = nil, nil, nil, nil
	r.Runs, r.Attempts = []core.Run{}, []core.Attempt{}
}

// requeueWithThread starts the report again after a reply, keeping the runs
// already in the case file: the reporter's answer is new information, not a
// reason to forget what Kane saw.
func requeueWithThread(r *core.Report) {
	r.Status = core.StatusQueued
	r.Check, r.Proof, r.Problem, r.ClosedAt = nil, nil, nil, nil
}

// Retry queues a report again from the start.
func (w *Worker) Retry(id string) error {
	_, err := w.o.Store.Update(id, func(r *core.Report) error {
		switch r.Status {
		case core.StatusError, core.StatusEscalated, core.StatusUnreproduced, core.StatusNeedsInfo:
		default:
			return &Conflict{fmt.Sprintf("A report that is %s cannot be retried; only errored, escalated, unreproduced or waiting reports can.", strings.ReplaceAll(string(r.Status), "_", " "))}
		}
		resetWork(r)
		return nil
	})
	if err != nil {
		return err
	}
	w.emit(id, core.EvReportRequeued, "", "A person retried this report; it starts again from the report.", nil)
	w.enqueue(job{id: id})
	return nil
}

// Approve lets a reproduced report go on to fixing when autonomy is "ask".
func (w *Worker) Approve(id string) error {
	_, err := w.o.Store.Update(id, func(r *core.Report) error {
		if r.Status != core.StatusAwaitingApproval {
			return &Conflict{"This report is not waiting for approval."}
		}
		r.Status = core.StatusQueued
		return nil
	})
	if err != nil {
		return err
	}
	w.emit(id, core.EvReportStatus, "fix", "A person approved the fix; it is queued.", map[string]any{"status": string(core.StatusQueued)})
	w.enqueue(job{id: id, resume: true})
	return nil
}

// Reply adds the reporter's answer and queues the report for triage again.
func (w *Worker) Reply(id, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return &Conflict{"The reply is empty."}
	}
	_, err := w.o.Store.Update(id, func(r *core.Report) error {
		if r.Status != core.StatusNeedsInfo {
			return &Conflict{"This report is not waiting for an answer."}
		}
		r.Thread = append(r.Thread, core.Message{From: "reporter", Text: text, At: time.Now().UTC()})
		requeueWithThread(r)
		return nil
	})
	if err != nil {
		return err
	}
	w.emit(id, core.EvReportReply, "", "The reporter answered: "+view.Clip(text, 160), nil)
	w.emit(id, core.EvReportRequeued, "", "Queued again with the reporter's answer.", nil)
	w.enqueue(job{id: id})
	return nil
}

// RunSuite queues a run of every suite test.
func (w *Worker) RunSuite() error {
	w.mu.Lock()
	if w.suiteRun {
		w.mu.Unlock()
		return &Conflict{"The suite is already queued or running."}
	}
	if len(w.o.Store.Suite()) == 0 {
		w.mu.Unlock()
		return &Conflict{"The suite is empty: every proven fix adds its check to it."}
	}
	w.suiteRun = true
	w.mu.Unlock()
	w.enqueue(job{})
	return nil
}

// Reset stops the current job, empties the queue, runs fn while the worker
// is held and then lets it go on.
func (w *Worker) Reset(fn func() error) error {
	w.mu.Lock()
	w.paused = true
	w.queue = nil
	w.suiteRun = false
	c := w.cur
	w.mu.Unlock()
	if c != nil {
		c.cancel()
		<-c.done
	}
	err := fn()
	w.mu.Lock()
	w.paused = false
	w.followUp = map[string]string{}
	w.mu.Unlock()
	w.poke()
	return err
}

// Recover handles reports left behind by a crash or a stop: queued ones go
// back in line, ones that were being worked on become errors with the
// baseline restored.
func (w *Worker) Recover(ctx context.Context) {
	reports := w.o.Store.List()
	sort.Slice(reports, func(i, j int) bool { return reports[i].UpdatedAt.Before(reports[j].UpdatedAt) })
	for _, r := range reports {
		switch {
		case r.Status == core.StatusQueued:
			resume := r.Check != nil && len(r.Runs) > 0 && lastFailing(r) != nil
			w.enqueue(job{id: r.ID, resume: resume})
		case r.Status.Active():
			w.interrupt(ctx, r.ID, "Fixpoint was restarted while this was in progress.")
		}
	}
}

func stageOf(s core.Status) string {
	switch s {
	case core.StatusTriaging, core.StatusQueued:
		return "triage"
	case core.StatusReproducing:
		return "reproduce"
	case core.StatusFixing:
		return "fix"
	case core.StatusVerifying:
		return "verify"
	case core.StatusRegressing:
		return "regress"
	}
	return ""
}

// interrupt turns an in-progress report into a retryable error.
func (w *Worker) interrupt(ctx context.Context, id, msg string) {
	restored := w.restoreBaseline(ctx, id)
	if restored {
		// The files are back; a stale baseline must not be restored again
		// later over fixes other reports made in the meantime.
		w.dropSnapshots(id)
	}
	now := time.Now().UTC()
	r, err := w.update(id, func(r *core.Report) {
		for i := range r.Runs {
			if r.Runs[i].Status == core.RunRunning {
				r.Runs[i].Status = core.RunErrored
				r.Runs[i].Error = "Fixpoint stopped during this run."
				r.Runs[i].EndedAt = &now
			}
		}
		for i := range r.Attempts {
			if r.Attempts[i].Status == core.AttemptRunning {
				r.Attempts[i].Status = core.AttemptAgentError
				r.Attempts[i].Note = "Fixpoint stopped during this attempt."
				r.Attempts[i].EndedAt = &now
			}
		}
		remedy := "Retry this report."
		if restored {
			remedy = "The app's files were put back as they were before the first attempt. Retry this report."
		}
		r.Problem = &core.Problem{Stage: stageOf(r.Status), Message: msg, Remedy: remedy, Retryable: true}
		r.Status = core.StatusError
	})
	if err != nil {
		return
	}
	w.emit(id, core.EvReportError, r.Problem.Stage, msg, map[string]any{"status": string(core.StatusError)})
}

// Run processes jobs until ctx ends.
func (w *Worker) Run(ctx context.Context) {
	for {
		j, ok := w.next(ctx)
		if !ok {
			return
		}
		jctx, cancel := context.WithCancel(ctx)
		c := &current{job: j, cancel: cancel, done: make(chan struct{})}
		w.mu.Lock()
		w.cur = c
		w.mu.Unlock()
		w.emit("", core.EvSystemChanged, "", "The worker started on the next job.", map[string]any{"busy": true})
		if j.id == "" {
			w.runSuite(jctx)
		} else {
			w.processReport(jctx, j)
		}
		w.mu.Lock()
		resetting := w.paused
		w.mu.Unlock()
		if jctx.Err() != nil && j.id != "" && !resetting {
			if r, err := w.o.Store.Get(j.id); err == nil && r.Status.Active() && r.Status != core.StatusQueued {
				bg, stop := context.WithTimeout(context.Background(), time.Minute)
				w.interrupt(bg, j.id, "Fixpoint was stopped while this was in progress.")
				stop()
			}
		}
		cancel()
		w.mu.Lock()
		w.cur = nil
		if j.id == "" {
			w.suiteRun = false
		}
		w.mu.Unlock()
		close(c.done)
		w.emit("", core.EvSystemChanged, "", "The worker finished a job.", map[string]any{"busy": false})
	}
}

func (w *Worker) next(ctx context.Context) (job, bool) {
	for {
		w.mu.Lock()
		if !w.paused && len(w.queue) > 0 {
			j := w.queue[0]
			w.queue = w.queue[1:]
			w.mu.Unlock()
			return j, true
		}
		w.mu.Unlock()
		select {
		case <-ctx.Done():
			return job{}, false
		case <-w.wake:
		}
	}
}

func (w *Worker) snapDir(id, name string) string {
	return filepath.Join(w.o.SnapshotsDir, id, name)
}

// restoreBaseline puts the target back as it was before attempt 1, if a
// baseline exists, and restarts the app. It reports whether it restored.
func (w *Worker) restoreBaseline(ctx context.Context, id string) bool {
	base := w.snapDir(id, "baseline")
	if !snapshot.Exists(base) {
		return false
	}
	if err := snapshot.Restore(base, w.o.Target.Dir, w.o.Exclude...); err != nil {
		log.Printf("fixpoint: %v", err)
		return false
	}
	if diffs, _ := snapshot.Diff(base, w.o.Target.Dir, w.o.Exclude...); len(diffs) > 0 {
		log.Printf("fixpoint: rollback of %s left %d files different", id, len(diffs))
	}
	if err := w.restart(ctx); err != nil {
		log.Printf("fixpoint: restart after rollback: %v", err)
	}
	return true
}

func (w *Worker) dropSnapshots(id string) {
	_ = os.RemoveAll(filepath.Join(w.o.SnapshotsDir, id))
}
