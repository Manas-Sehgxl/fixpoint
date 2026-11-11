package view

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
)

func states(r core.Report) string {
	var sb strings.Builder
	for _, n := range Track(r) {
		sb.WriteString(string(n.State[0]))
	}
	return sb.String()
}

func TestTrack(t *testing.T) {
	patched := []core.Attempt{{N: 1, Files: []core.FileDiff{{Path: "x"}}}}
	cases := []struct {
		name string
		r    core.Report
		want string // first letter of each node's state
	}{
		{"queued", core.Report{Status: core.StatusQueued}, "apppp"},
		{"triaging", core.Report{Status: core.StatusTriaging}, "apppp"},
		{"reproducing", core.Report{Status: core.StatusReproducing}, "dappp"},
		{"needs info waits on the reporter", core.Report{Status: core.StatusNeedsInfo}, "dwppp"},
		{"awaiting approval waits on a person", core.Report{Status: core.StatusAwaitingApproval}, "ddwpp"},
		{"fixing", core.Report{Status: core.StatusFixing}, "ddapp"},
		{"verifying", core.Report{Status: core.StatusVerifying}, "dddap"},
		{"regressing", core.Report{Status: core.StatusRegressing}, "dddap"},
		{"proven", core.Report{Status: core.StatusProven}, "ddddd"},
		{"unreproduced", core.Report{Status: core.StatusUnreproduced}, "dpppp"},
		{"escalated after a patch", core.Report{Status: core.StatusEscalated, Attempts: patched}, "dddfp"},
		{"escalated before any patch", core.Report{Status: core.StatusEscalated}, "ddfpp"},
		{"error in triage", core.Report{Status: core.StatusError, Problem: &core.Problem{Stage: "triage"}}, "dfppp"},
		{"error in fixing", core.Report{Status: core.StatusError, Problem: &core.Problem{Stage: "fix"}}, "ddfpp"},
		{"error in verify", core.Report{Status: core.StatusError, Problem: &core.Problem{Stage: "verify"}}, "dddfp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := states(c.r); got != c.want {
				t.Errorf("got %s want %s", got, c.want)
			}
			for _, n := range Track(c.r) {
				if n.Key == "" || n.Label == "" {
					t.Errorf("node without a label: %+v", n)
				}
			}
		})
	}
}

func TestPublicWording(t *testing.T) {
	for status, want := range map[core.Status][2]string{
		core.StatusQueued:       {"received", "Report received"},
		core.StatusTriaging:     {"received", "Reading your report"},
		core.StatusReproducing:  {"reproducing", "Trying it in a real browser"},
		core.StatusNeedsInfo:    {"question", "We could not reproduce it yet"},
		core.StatusFixing:       {"fixing", "We saw the problem too"},
		core.StatusVerifying:    {"verifying", "Testing the fix"},
		core.StatusRegressing:   {"verifying", "Testing the fix"},
		core.StatusProven:       {"fixed", "Fixed and verified"},
		core.StatusUnreproduced: {"closed", "We could not reproduce this"},
		core.StatusEscalated:    {"handed_off", "A developer is taking this one"},
		core.StatusError:        {"trouble", "Something went wrong on our side"},
	} {
		v := Public(core.Report{Status: status})
		if v.Stage != want[0] || v.Headline != want[1] {
			t.Errorf("%s: %+v", status, v)
		}
		if v.Step < 1 || v.Step > 4 {
			t.Errorf("%s: step %d", status, v.Step)
		}
	}
	// A question becomes the detail, and the shots come from the runs.
	r := core.Report{Status: core.StatusNeedsInfo,
		Thread: []core.Message{{From: "fixpoint", Text: "Old question"}, {From: "reporter", Text: "An answer"}, {From: "fixpoint", Text: "Which item?"}}}
	v := Public(r)
	if v.Question != "Which item?" || v.Detail != "Which item?" {
		t.Errorf("question: %+v", v)
	}
	proven := core.Report{Status: core.StatusProven,
		Runs: []core.Run{
			{ID: "run_1", Kind: core.RunReproduce, Status: core.RunFailed, Evidence: &core.Evidence{Shots: []core.Shot{{Step: 1, URL: "/a/1.png"}, {Step: 2, URL: "/a/2.png"}}}},
			{ID: "run_2", Kind: core.RunVerify, Status: core.RunPassed, Evidence: &core.Evidence{Shots: []core.Shot{{Step: 5, URL: "/b/5.png"}}}},
		},
		Proof: &core.Proof{RedRunID: "run_1", GreenRunID: "run_2"}}
	v = Public(proven)
	if v.BeforeShot != "/a/2.png" || v.AfterShot != "/b/5.png" {
		t.Errorf("shots: %+v", v)
	}
}

func TestFillAndNoNullSlices(t *testing.T) {
	r := core.Report{ID: "r_1", Status: core.StatusQueued, Text: "x"}
	Fill(&r, 3)
	if r.QueuePosition != 3 || len(r.Track) != 5 {
		t.Errorf("fill: %+v", r)
	}
	r.Status = core.StatusProven
	Fill(&r, 3)
	if r.QueuePosition != 0 {
		t.Error("a proven report should have no queue position")
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "null") {
		t.Errorf("null in the response: %s", b)
	}
	run := core.Run{ID: "run_1"}
	NormalizeRun(&run)
	b, _ = json.Marshal(run)
	if strings.Contains(string(b), "null") {
		t.Errorf("null in a run: %s", b)
	}
	if evs := NormalizeEvents(nil); evs == nil || len(evs) != 0 {
		t.Error("nil events should become an empty list")
	}
	evs := NormalizeEvents([]core.Event{{Type: "a"}})
	if evs[0].Data == nil {
		t.Error("event data should never be null")
	}
}

func TestStatsAndSummary(t *testing.T) {
	now := time.Now()
	reports := []core.Report{
		{Status: core.StatusProven, Proof: &core.Proof{TimeToProofS: 100}, Runs: []core.Run{{Credits: 5}}, Attempts: []core.Attempt{{Agent: core.AgentCall{CostUSD: 0.1}}}},
		{Status: core.StatusProven, Proof: &core.Proof{TimeToProofS: 300}},
		{Status: core.StatusProven, Proof: &core.Proof{TimeToProofS: 200}},
		{Status: core.StatusFixing},
		{Status: core.StatusNeedsInfo},
		{Status: core.StatusError},
	}
	s := Stats(reports, 3)
	if s.Reports != 6 || s.Proven != 3 || s.InProgress != 1 || s.NeedsYou != 2 || s.SuiteSize != 3 {
		t.Errorf("stats %+v", s)
	}
	if s.MedianTimeToProofS != 200 || s.CreditsUsed != 5 || s.AgentCostUSD != 0.1 {
		t.Errorf("stats %+v", s)
	}
	if m := Stats([]core.Report{
		{Status: core.StatusProven, Proof: &core.Proof{TimeToProofS: 100}},
		{Status: core.StatusProven, Proof: &core.Proof{TimeToProofS: 200}},
	}, 0).MedianTimeToProofS; m != 150 {
		t.Errorf("even median %v", m)
	}

	r := core.Report{ID: "r_1", Seq: 7, Status: core.StatusProven, Title: "T", Text: strings.Repeat("word ", 80),
		MaxAttempts: 3, CreatedAt: now, UpdatedAt: now, Proof: &core.Proof{TimeToProofS: 42},
		Attempts: []core.Attempt{{N: 1, Status: core.AttemptRevisedCheck}, {N: 2, Status: core.AttemptVerified}}}
	sum := Summary(r)
	if sum.Attempts != 1 {
		t.Errorf("a corrected check should not count as an attempt: %d", sum.Attempts)
	}
	if len([]rune(sum.Text)) > 201 || sum.TimeToProofS != 42 || len(sum.Track) != 5 {
		t.Errorf("summary %+v", sum)
	}
}

func TestClip(t *testing.T) {
	for in, want := range map[string]string{
		"short":                     "short",
		"  trimmed  ":               "trimmed",
		"the quick brown fox jumps": "the quick brown…",
	} {
		n := 16
		if len(in) < 10 {
			n = 100
		}
		if got := Clip(in, n); got != want {
			t.Errorf("Clip(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Clip("₹₹₹₹₹₹", 3); got != "₹₹₹…" {
		t.Errorf("runes: %q", got)
	}
}
