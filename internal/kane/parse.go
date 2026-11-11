// Package kane runs Kane CLI, reads its NDJSON stream and its evidence packs,
// and probes whether it is installed and signed in.
package kane

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
)

// Parser reads Kane's stdout one line at a time, for both `run` and
// `testmd run`. Lines that are not JSON and unknown event types are skipped.
type Parser struct {
	// OnStep, when set, is called with the full step list after every change.
	OnStep func(steps []core.Step)

	CLIVersion string
	Surface    string
	Steps      []core.Step
	Errors     []string
	RunEnd     *RunEnd
	TestMD     *TestMDResult

	testmdStep int
}

// RunEnd is Kane's closing event of a run.
type RunEnd struct {
	Status      string          `json:"status"`
	Summary     string          `json:"summary"`
	OneLiner    string          `json:"one_liner"`
	Reason      string          `json:"reason"`
	Duration    float64         `json:"duration"`
	Credits     *float64        `json:"credits_consumed"`
	OldCredits  *float64        `json:"credits"`
	FinalState  map[string]any  `json:"final_state"`
	SessionDir  string          `json:"session_dir"`
	TestURL     string          `json:"test_url"`
	ResultCode  json.RawMessage `json:"result_code"`
	VerdictJSON json.RawMessage `json:"verdict"`
}

// TestMDResult is what the testmd stream says about the whole replay.
type TestMDResult struct {
	OverallStatus   string
	DurationS       float64
	SessionID       string
	ShareURL        string
	ReplayDecisions int
	AuthorDecisions int
	Done            bool
}

// Replayed reports whether the test ran from its recording.
func (t *TestMDResult) Replayed() bool {
	return t != nil && t.ReplayDecisions > 0 && t.AuthorDecisions == 0
}

type line struct {
	Type       *string         `json:"type"`
	Step       *json.Number    `json:"step"`
	Status     string          `json:"status"`
	Remark     string          `json:"remark"`
	Message    string          `json:"message"`
	CLIVersion string          `json:"cli_version"`
	Surface    string          `json:"surface"`
	StepIndex  *int            `json:"step_index"`
	Heading    string          `json:"heading"`
	Overall    string          `json:"overall_status"`
	DurationS  float64         `json:"duration_s"`
	SessionID  string          `json:"session_id"`
	ShareURL   string          `json:"share_url"`
	StepsInfo  json.RawMessage `json:"steps"`
}

// Line feeds one line of stdout to the parser.
func (p *Parser) Line(b []byte) {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '{' {
		return
	}
	var l line
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&l); err != nil {
		return
	}
	if l.Type == nil {
		if l.Step != nil || l.Status != "" {
			p.progress(l)
		}
		return
	}
	switch *l.Type {
	case "stream_start":
		p.CLIVersion = l.CLIVersion
		p.Surface = l.Surface
	case "error":
		if l.Message != "" {
			p.Errors = append(p.Errors, l.Message)
		}
	case "run_end":
		var re RunEnd
		if json.Unmarshal(b, &re) == nil {
			// In a testmd replay each step has its own run_end; keep the
			// first failing one as the explanation, else the last.
			if p.RunEnd == nil || p.RunEnd.Status != "failed" {
				p.RunEnd = &re
			}
		}
	case "test_md_step_start":
		p.testmd().Done = false
		p.testmdStep++
		remark := l.Heading
		if remark == "" {
			remark = fmt.Sprintf("Test step %d", p.testmdStep)
		}
		p.Steps = append(p.Steps, core.Step{N: len(p.Steps) + 1, Status: "running", Remark: remark})
		p.changed()
	case "test_md_step_end":
		if n := len(p.Steps); n > 0 {
			p.Steps[n-1].Status = stepStatus(l.Status)
			p.changed()
		}
	case "test_md_summary":
		t := p.testmd()
		t.OverallStatus = l.Overall
		t.DurationS = l.DurationS
		var counts struct {
			Replay int `json:"replay_decisions"`
			Author int `json:"author_decisions"`
		}
		if json.Unmarshal(l.StepsInfo, &counts) == nil {
			t.ReplayDecisions, t.AuthorDecisions = counts.Replay, counts.Author
		}
	case "test_md_done":
		t := p.testmd()
		t.OverallStatus = l.Overall
		if l.DurationS > 0 {
			t.DurationS = l.DurationS
		}
		t.SessionID = l.SessionID
		t.ShareURL = l.ShareURL
		t.Done = true
	}
}

func (p *Parser) testmd() *TestMDResult {
	if p.TestMD == nil {
		p.TestMD = &TestMDResult{}
	}
	return p.TestMD
}

func stepStatus(s string) string {
	switch strings.ToLower(s) {
	case "done", "passed", "pass", "success":
		return "done"
	case "failed", "fail", "error":
		return "failed"
	}
	return "running"
}

// progress handles a line without a type. Kane's step numbers can repeat or
// skip, so steps are numbered by their order here.
func (p *Parser) progress(l line) {
	if p.Surface == "testmd" || p.testmdStep > 0 {
		// Inner progress of a replayed test step: the test step already
		// stands for it.
		return
	}
	status := stepStatus(l.Status)
	n := len(p.Steps)
	if n > 0 && p.Steps[n-1].Status == "running" {
		p.Steps[n-1].Status = status
		if l.Remark != "" {
			p.Steps[n-1].Remark = l.Remark
		}
	} else {
		p.Steps = append(p.Steps, core.Step{N: n + 1, Status: status, Remark: l.Remark})
	}
	p.changed()
}

func (p *Parser) changed() {
	if p.OnStep != nil {
		p.OnStep(append([]core.Step(nil), p.Steps...))
	}
}

// Fill copies what the stream said into run.
func (p *Parser) Fill(run *core.Run) {
	run.CLIVersion = p.CLIVersion
	run.Steps = append([]core.Step{}, p.Steps...)
	if re := p.RunEnd; re != nil {
		run.Summary = re.Summary
		run.OneLiner = re.OneLiner
		run.Reason = re.Reason
		run.DurationS = re.Duration
		switch {
		case re.Credits != nil:
			run.Credits = *re.Credits
		case re.OldCredits != nil:
			run.Credits = *re.OldCredits
		}
		run.FinalState = re.FinalState
		run.TestURL = re.TestURL
		run.BugVerdict = re.Verdict()
	}
	if t := p.TestMD; t != nil {
		if t.DurationS > 0 {
			run.DurationS = t.DurationS
		}
		if run.TestURL == "" {
			run.TestURL = t.ShareURL
		}
	}
	if run.FinalState == nil {
		run.FinalState = map[string]any{}
	}
}

// Verdict returns Kane's bug verdict when bug detection confirmed one.
func (re *RunEnd) Verdict() *core.BugVerdict {
	if len(re.VerdictJSON) == 0 || string(re.VerdictJSON) == "null" {
		return nil
	}
	var v struct {
		Confirmed  bool            `json:"confirmed"`
		Family     string          `json:"family"`
		Category   string          `json:"category"`
		Severity   string          `json:"severity"`
		OneLiner   string          `json:"one_liner"`
		Confidence json.RawMessage `json:"confidence"`
	}
	if json.Unmarshal(re.VerdictJSON, &v) != nil {
		return nil
	}
	conf := strings.TrimSpace(string(v.Confidence))
	if s, err := strconv.Unquote(conf); err == nil {
		conf = s
	}
	if conf == "null" {
		conf = ""
	}
	return &core.BugVerdict{Confirmed: v.Confirmed, Family: v.Family, Category: v.Category,
		Severity: v.Severity, OneLiner: v.OneLiner, Confidence: conf}
}

// lineWriter splits a byte stream into lines for the parser.
type lineWriter struct {
	buf []byte
	fn  func([]byte)
}

func (w *lineWriter) Write(b []byte) (int, error) {
	w.buf = append(w.buf, b...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.fn(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
	// A runaway line without a newline is not NDJSON we can use.
	if len(w.buf) > 4<<20 {
		w.buf = w.buf[:0]
	}
	return len(b), nil
}

func (w *lineWriter) flush() {
	if len(w.buf) > 0 {
		w.fn(w.buf)
		w.buf = nil
	}
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.buf = append(t.buf, b...)
	if len(t.buf) > t.max {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-t.max:]...)
	}
	return len(b), nil
}

func (t *tailBuffer) String() string { return string(t.buf) }
