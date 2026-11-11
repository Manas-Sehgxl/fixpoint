// Package view computes the fields of a report that are derived on read (the
// progress track and the reporter-facing wording), the board summary and
// statistics, and makes sure no slice is encoded as null.
package view

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
)

var trackKeys = []struct{ key, label string }{
	{"reported", "Reported"},
	{"reproduced", "Reproduced"},
	{"patched", "Patched"},
	{"verified", "Verified"},
	{"proven", "Proven"},
}

// Track returns the five-node progress track for a report.
func Track(r core.Report) []core.TrackNode {
	states := make([]string, 5)
	for i := range states {
		states[i] = "pending"
	}
	done := func(n int) {
		for i := 0; i < n; i++ {
			states[i] = "done"
		}
	}
	switch r.Status {
	case core.StatusQueued, core.StatusTriaging:
		states[0] = "active"
	case core.StatusReproducing:
		done(1)
		states[1] = "active"
	case core.StatusNeedsInfo:
		done(1)
		states[1] = "waiting"
	case core.StatusAwaitingApproval:
		done(2)
		states[2] = "waiting"
	case core.StatusFixing:
		done(2)
		states[2] = "active"
	case core.StatusVerifying, core.StatusRegressing:
		done(3)
		states[3] = "active"
	case core.StatusProven:
		done(5)
	case core.StatusUnreproduced:
		done(1)
	case core.StatusEscalated:
		if anyPatch(r) {
			done(3)
			states[3] = "failed"
		} else {
			done(2)
			states[2] = "failed"
		}
	case core.StatusError:
		at := 1
		if r.Problem != nil {
			switch r.Problem.Stage {
			case "fix":
				at = 2
			case "verify", "regress":
				at = 3
			}
		}
		done(at)
		states[at] = "failed"
	}
	out := make([]core.TrackNode, 5)
	for i, k := range trackKeys {
		out[i] = core.TrackNode{Key: k.key, Label: k.label, State: states[i]}
	}
	return out
}

func anyPatch(r core.Report) bool {
	for _, a := range r.Attempts {
		if len(a.Files) > 0 {
			return true
		}
	}
	return false
}

type wording struct {
	stage            string
	step             int
	headline, detail string
}

var public = map[core.Status]wording{
	core.StatusQueued:           {"received", 1, "Report received", "It is in line. This page updates by itself."},
	core.StatusTriaging:         {"received", 1, "Reading your report", "Working out exactly what to test."},
	core.StatusReproducing:      {"reproducing", 2, "Trying it in a real browser", "We are repeating what you did to see the problem ourselves."},
	core.StatusNeedsInfo:        {"question", 2, "We could not reproduce it yet", ""},
	core.StatusAwaitingApproval: {"fixing", 2, "We saw the problem too", "A developer is about to start on a fix."},
	core.StatusFixing:           {"fixing", 3, "We saw the problem too", "A fix is being written."},
	core.StatusVerifying:        {"verifying", 3, "Testing the fix", "The same steps are being run again in a real browser."},
	core.StatusRegressing:       {"verifying", 3, "Testing the fix", "Making sure nothing else broke."},
	core.StatusProven:           {"fixed", 4, "Fixed and verified", "Reload the page to get the fix. Thank you for reporting it."},
	core.StatusUnreproduced:     {"closed", 2, "We could not reproduce this", "We tried, and the app behaved correctly for us. Send a new report if it happens again."},
	core.StatusEscalated:        {"handed_off", 3, "A developer is taking this one", "Automatic fixes did not hold, so a person has everything we found."},
	core.StatusError:            {"trouble", 1, "Something went wrong on our side", "The team has been told. Nothing more is needed from you."},
}

// Public returns the reporter-facing view of a report.
func Public(r core.Report) core.PublicView {
	w, ok := public[r.Status]
	if !ok {
		w = public[core.StatusQueued]
	}
	v := core.PublicView{Stage: w.stage, Step: w.step, Headline: w.headline, Detail: w.detail}
	if r.Status == core.StatusNeedsInfo {
		v.Question = LastQuestion(r)
		v.Detail = v.Question
	}
	v.BeforeShot, v.AfterShot = Shots(r)
	return v
}

// LastQuestion returns the most recent question Fixpoint asked the reporter.
func LastQuestion(r core.Report) string {
	for i := len(r.Thread) - 1; i >= 0; i-- {
		if r.Thread[i].From == "fixpoint" {
			return r.Thread[i].Text
		}
	}
	return ""
}

// Shots returns the failing reproduce run's last screenshot and the passing
// verify run's last screenshot, when they exist.
func Shots(r core.Report) (before, after string) {
	for _, run := range r.Runs {
		if run.Kind == core.RunReproduce && run.Status == core.RunFailed {
			before = lastShot(run)
		}
	}
	if r.Proof != nil {
		for _, run := range r.Runs {
			if run.ID == r.Proof.GreenRunID {
				after = lastShot(run)
			}
		}
	}
	return before, after
}

func lastShot(run core.Run) string {
	if run.Evidence == nil || len(run.Evidence.Shots) == 0 {
		return ""
	}
	return run.Evidence.Shots[len(run.Evidence.Shots)-1].URL
}

// Fill sets the derived fields and replaces nil slices with empty ones.
func Fill(r *core.Report, queuePos int) {
	Normalize(r)
	r.QueuePosition = 0
	if r.Status == core.StatusQueued {
		r.QueuePosition = queuePos
	}
	r.Track = Track(*r)
	r.Public = Public(*r)
}

// Normalize replaces every nil slice and map in r with an empty one.
func Normalize(r *core.Report) {
	if r.Thread == nil {
		r.Thread = []core.Message{}
	}
	if r.Runs == nil {
		r.Runs = []core.Run{}
	}
	if r.Attempts == nil {
		r.Attempts = []core.Attempt{}
	}
	if r.Track == nil {
		r.Track = []core.TrackNode{}
	}
	if r.Context.Breadcrumbs == nil {
		r.Context.Breadcrumbs = []core.Breadcrumb{}
	}
	if r.Context.ConsoleErrors == nil {
		r.Context.ConsoleErrors = []string{}
	}
	for i := range r.Runs {
		NormalizeRun(&r.Runs[i])
	}
	for i := range r.Attempts {
		a := &r.Attempts[i]
		if a.Files == nil {
			a.Files = []core.FileDiff{}
		}
		if a.RegressionRunIDs == nil {
			a.RegressionRunIDs = []string{}
		}
	}
	if r.Proof != nil && r.Proof.Files == nil {
		r.Proof.Files = []core.FileDiff{}
	}
}

// NormalizeRun replaces every nil slice and map in a run with an empty one.
func NormalizeRun(run *core.Run) {
	if run.Steps == nil {
		run.Steps = []core.Step{}
	}
	if run.FinalState == nil {
		run.FinalState = map[string]any{}
	}
	if e := run.Evidence; e != nil {
		if e.Shots == nil {
			e.Shots = []core.Shot{}
		}
		if e.ConsoleErrors == nil {
			e.ConsoleErrors = []string{}
		}
	}
}

// NormalizeEvents replaces nil event data and a nil list with empty ones.
func NormalizeEvents(evs []core.Event) []core.Event {
	if evs == nil {
		return []core.Event{}
	}
	for i := range evs {
		if evs[i].Data == nil {
			evs[i].Data = map[string]any{}
		}
	}
	return evs
}

// CountedAttempts is the number of fix attempts that count towards the
// limit: a corrected check is not a failed fix.
func CountedAttempts(r core.Report) int {
	n := 0
	for _, a := range r.Attempts {
		if a.Status != core.AttemptRevisedCheck {
			n++
		}
	}
	return n
}

// Summary returns the board row for a filled report.
func Summary(r core.Report) core.ReportSummary {
	s := core.ReportSummary{
		ID: r.ID, Seq: r.Seq, Status: r.Status, Title: r.Title,
		Text: Clip(r.Text, 200), Attempts: CountedAttempts(r), MaxAttempts: r.MaxAttempts,
		QueuePosition: r.QueuePosition, Simulated: r.Simulated, Track: r.Track,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.Proof != nil {
		s.TimeToProofS = r.Proof.TimeToProofS
	}
	if s.Track == nil {
		s.Track = Track(r)
	}
	return s
}

// PublicReport returns the reporter's view of a filled report.
func PublicReport(r core.Report) core.PublicReport {
	th := r.Thread
	if th == nil {
		th = []core.Message{}
	}
	return core.PublicReport{
		ID: r.ID, Seq: r.Seq, Text: r.Text, Thread: th, View: Public(r),
		Done: r.Status.Done(), Simulated: r.Simulated,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// Stats computes the board's summary line.
func Stats(reports []core.Report, suiteSize int) core.Stats {
	s := core.Stats{Reports: len(reports), SuiteSize: suiteSize}
	var times []float64
	for _, r := range reports {
		switch {
		case r.Status == core.StatusProven:
			s.Proven++
			if r.Proof != nil {
				times = append(times, r.Proof.TimeToProofS)
			}
		case r.Status.Active():
			s.InProgress++
		}
		if r.Status.NeedsPerson() {
			s.NeedsYou++
		}
		for _, run := range r.Runs {
			s.CreditsUsed += run.Credits
		}
		for _, a := range r.Attempts {
			s.AgentCostUSD += a.Agent.CostUSD
		}
	}
	if len(times) > 0 {
		sort.Float64s(times)
		m := len(times) / 2
		if len(times)%2 == 1 {
			s.MedianTimeToProofS = times[m]
		} else {
			s.MedianTimeToProofS = (times[m-1] + times[m]) / 2
		}
	}
	return s
}

// Clip shortens s to at most n characters on a word boundary, adding an
// ellipsis when it cut something.
func Clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	rs := []rune(s)[:n]
	cut := string(rs)
	if i := strings.LastIndexAny(cut, " \n\t"); i > n/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}
