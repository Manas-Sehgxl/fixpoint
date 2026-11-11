// Package core holds the types every part of Fixpoint shares: reports, Kane
// runs, fix attempts, the regression suite and the events that describe
// progress. The JSON encoding of these types is the HTTP API contract, so the
// console, the widget and the engine all agree by construction.
//
// Field names are snake_case in JSON. Slices are never null in responses:
// encode an empty slice as [].
package core

import "time"

// Status is where a report is in the loop.
type Status string

const (
	StatusQueued           Status = "queued"            // waiting for the worker
	StatusTriaging         Status = "triaging"          // the coding agent is writing the check
	StatusReproducing      Status = "reproducing"       // Kane is running the check for the first time
	StatusNeedsInfo        Status = "needs_info"        // could not reproduce; waiting for the reporter
	StatusAwaitingApproval Status = "awaiting_approval" // reproduced; waiting for a person to allow a fix
	StatusFixing           Status = "fixing"            // the coding agent is patching the app
	StatusVerifying        Status = "verifying"         // Kane is re-running the check after a patch
	StatusRegressing       Status = "regressing"        // Kane is re-running the checks of earlier fixes
	StatusProven           Status = "proven"            // done: fixed, with proof
	StatusUnreproduced     Status = "unreproduced"      // done: closed, never reproduced
	StatusEscalated        Status = "escalated"         // done: handed to a person, patches rolled back
	StatusError            Status = "error"             // stopped by a tool problem; can be retried
)

// Active reports are being worked on by the worker right now or are queued.
func (s Status) Active() bool {
	switch s {
	case StatusQueued, StatusTriaging, StatusReproducing, StatusFixing, StatusVerifying, StatusRegressing:
		return true
	}
	return false
}

// Done reports will not change unless someone retries them.
func (s Status) Done() bool {
	return s == StatusProven || s == StatusUnreproduced || s == StatusEscalated
}

// NeedsPerson is true when the next move belongs to a human.
func (s Status) NeedsPerson() bool {
	switch s {
	case StatusNeedsInfo, StatusAwaitingApproval, StatusEscalated, StatusError:
		return true
	}
	return false
}

// Report is one bug report and everything Fixpoint did with it.
type Report struct {
	ID       string    `json:"id"`  // unguessable, e.g. "r_7f3k2m9qxa"; used in public URLs
	Seq      int       `json:"seq"` // 1, 2, 3 ... shown as #7
	Status   Status    `json:"status"`
	Title    string    `json:"title"` // short title: from triage, else the first words of Text
	Text     string    `json:"text"`  // the reporter's own words
	Reporter Reporter  `json:"reporter"`
	Context  Context   `json:"context"` // what the widget captured with the report
	Thread   []Message `json:"thread"`  // questions to the reporter and their replies, oldest first

	Check    *Check    `json:"check,omitempty"`   // the plain-English check Kane runs
	Runs     []Run     `json:"runs"`              // every Kane run for this report, oldest first
	Attempts []Attempt `json:"attempts"`          // every fix attempt, oldest first
	Proof    *Proof    `json:"proof,omitempty"`   // set when Status is proven
	Problem  *Problem  `json:"problem,omitempty"` // set when Status is error or escalated

	MaxAttempts   int  `json:"max_attempts"`
	QueuePosition int  `json:"queue_position"` // 1 = next; 0 when not queued
	Simulated     bool `json:"simulated"`      // true when stub runners produced this report's runs

	Track  []TrackNode `json:"track"`  // the five-step progress track, derived
	Public PublicView  `json:"public"` // the wording shown to the reporter, derived

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
}

// ReportSummary is the row shown on the reports board.
type ReportSummary struct {
	ID            string      `json:"id"`
	Seq           int         `json:"seq"`
	Status        Status      `json:"status"`
	Title         string      `json:"title"`
	Text          string      `json:"text"` // trimmed to about 200 characters
	Attempts      int         `json:"attempts"`
	MaxAttempts   int         `json:"max_attempts"`
	TimeToProofS  float64     `json:"time_to_proof_s"` // 0 until proven
	QueuePosition int         `json:"queue_position"`
	Simulated     bool        `json:"simulated"`
	Track         []TrackNode `json:"track"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

// TrackNode is one step of the five-step progress track:
// reported, reproduced, patched, verified, proven.
type TrackNode struct {
	Key   string `json:"key"`   // reported | reproduced | patched | verified | proven
	Label string `json:"label"` // "Reported", "Reproduced", ...
	State string `json:"state"` // pending | active | done | failed | waiting
}

// Reporter is whoever filed the report. Both fields are optional.
type Reporter struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Context is what the widget captured when the report was sent.
type Context struct {
	PageURL       string       `json:"page_url"`
	UserAgent     string       `json:"user_agent"`
	ViewportW     int          `json:"viewport_w"`
	ViewportH     int          `json:"viewport_h"`
	Breadcrumbs   []Breadcrumb `json:"breadcrumbs"`    // the reporter's last actions, oldest first
	ConsoleErrors []string     `json:"console_errors"` // errors seen on the page before the report
}

// Breadcrumb is one thing the reporter did before reporting.
type Breadcrumb struct {
	Kind  string `json:"kind"`   // click | input | submit | navigate | error
	Text  string `json:"text"`   // plain English, e.g. Clicked "Add to cart" in "Masala Chai"
	AgoMS int64  `json:"ago_ms"` // how long before the report was sent
}

// Message is one entry in the conversation with the reporter.
type Message struct {
	From string    `json:"from"` // "fixpoint" or "reporter"
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// Check is the plain-English browser check written from the report. It must
// pass when the app behaves correctly and fail while the bug exists.
type Check struct {
	Title     string `json:"title"`
	Objective string `json:"objective"` // the Kane CLI objective, verbatim
	Expected  string `json:"expected"`  // one sentence: what correct behaviour is
	StartURL  string `json:"start_url"`
	WrittenBy string `json:"written_by"` // the coding agent's name, e.g. "Claude Code"
	Revision  int    `json:"revision"`   // 1, then 2 if the agent corrected its own check
	TestFile  string `json:"test_file"`  // saved replayable test, when one exists
}

// RunKind says why Kane ran.
type RunKind string

const (
	RunReproduce  RunKind = "reproduce"  // first run of the check; a failure means "reproduced"
	RunVerify     RunKind = "verify"     // the same check after a patch
	RunRegression RunKind = "regression" // an earlier fix's check, re-run after a patch
	RunSuite      RunKind = "suite"      // part of a manual "run the whole suite"
)

// RunStatus is the outcome of one Kane run.
type RunStatus string

const (
	RunRunning RunStatus = "running"
	RunPassed  RunStatus = "passed"  // exit 0
	RunFailed  RunStatus = "failed"  // exit 1: the check ran and did not pass
	RunErrored RunStatus = "error"   // exit 2: nothing ran (sign-in, Chrome, usage)
	RunTimeout RunStatus = "timeout" // exit 3, or our own deadline
)

// Run is one execution of Kane CLI.
type Run struct {
	ID          string    `json:"id"` // "run_1", "run_2" ... unique within the report
	Kind        RunKind   `json:"kind"`
	Attempt     int       `json:"attempt"`       // 0 for reproduce; the attempt number for verify/regression
	SuiteTestID string    `json:"suite_test_id"` // set for regression and suite runs
	Title       string    `json:"title"`         // what was checked, for display
	Objective   string    `json:"objective"`
	Status      RunStatus `json:"status"`
	ExitCode    int       `json:"exit_code"`

	// From Kane's closing event.
	Summary    string         `json:"summary"`
	OneLiner   string         `json:"one_liner"`
	Reason     string         `json:"reason"`
	DurationS  float64        `json:"duration_s"`
	Credits    float64        `json:"credits"`
	FinalState map[string]any `json:"final_state"` // values the check stored, e.g. {"observed": "₹540"}
	TestURL    string         `json:"test_url"`    // the run in TestMu AI Test Manager, when uploaded
	BugVerdict *BugVerdict    `json:"bug_verdict,omitempty"`

	Steps    []Step    `json:"steps"`
	Evidence *Evidence `json:"evidence,omitempty"`

	CLIVersion string `json:"cli_version"`
	Simulated  bool   `json:"simulated"`
	Error      string `json:"error"` // when Status is error or timeout: what happened and what to do

	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

// Step is one line of Kane's progress.
type Step struct {
	N      int    `json:"n"`
	Status string `json:"status"` // running | done | failed
	Remark string `json:"remark"`
}

// BugVerdict is Kane's own judgement, when its bug detection confirmed a
// product bug during the run.
type BugVerdict struct {
	Confirmed  bool   `json:"confirmed"`
	Family     string `json:"family"`
	Category   string `json:"category"`
	Severity   string `json:"severity"`
	OneLiner   string `json:"one_liner"`
	Confidence string `json:"confidence"`
}

// Evidence is what Fixpoint pulled out of the run's sealed evidence pack.
type Evidence struct {
	PackPath      string   `json:"pack_path"` // the .evidence file on disk
	Shots         []Shot   `json:"shots"`     // step screenshots, in step order
	FailureText   string   `json:"failure_text"`
	ConsoleErrors []string `json:"console_errors"`
}

// Shot is one step's screenshot, served under /artifacts/.
type Shot struct {
	Step         int    `json:"step"`
	URL          string `json:"url"`
	AnnotatedURL string `json:"annotated_url"` // the same shot with the acted-on element marked; may be empty
	Failed       bool   `json:"failed"`
}

// AttemptStatus is how one fix attempt ended.
type AttemptStatus string

const (
	AttemptRunning      AttemptStatus = "running"
	AttemptVerified     AttemptStatus = "verified"      // the check passed and nothing else broke
	AttemptStillFailing AttemptStatus = "still_failing" // the check still fails
	AttemptRegressed    AttemptStatus = "regressed"     // the check passed but an earlier fix broke
	AttemptNoChange     AttemptStatus = "no_change"     // the agent said "patched" but changed nothing
	AttemptRevisedCheck AttemptStatus = "revised_check" // the agent corrected the check instead of the app
	AttemptGaveUp       AttemptStatus = "gave_up"
	AttemptAgentError   AttemptStatus = "agent_error" // the agent could not be run or timed out
)

// Attempt is one pass of the coding agent at the fix, and what Kane said
// about it afterwards.
type Attempt struct {
	N         int           `json:"n"` // 1, 2, 3
	Status    AttemptStatus `json:"status"`
	RootCause string        `json:"root_cause"`
	Summary   string        `json:"summary"`
	Note      string        `json:"note"` // why it did not hold, in one sentence
	Agent     AgentCall     `json:"agent"`
	Files     []FileDiff    `json:"files"` // what this attempt changed

	VerifyRunID      string   `json:"verify_run_id"`
	RegressionRunIDs []string `json:"regression_run_ids"`

	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

// AgentCall records one invocation of the coding agent.
type AgentCall struct {
	Name       string  `json:"name"` // "Claude Code"
	DurationMS int64   `json:"duration_ms"`
	CostUSD    float64 `json:"cost_usd"`
	Turns      int     `json:"turns"`
	SessionID  string  `json:"session_id"`
}

// FileDiff is the change to one file.
type FileDiff struct {
	Path      string `json:"path"`   // relative to the target directory
	Change    string `json:"change"` // modified | added | deleted
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"` // unified diff for this file
}

// Proof is the record that closes a report: the failing run, the patch and
// the passing run.
type Proof struct {
	RedRunID         string     `json:"red_run_id"`
	GreenRunID       string     `json:"green_run_id"`
	Attempt          int        `json:"attempt"`
	TimeToProofS     float64    `json:"time_to_proof_s"` // report received to proven
	Files            []FileDiff `json:"files"`           // the whole change, baseline to final
	PatchURL         string     `json:"patch_url"`
	BeforeShot       string     `json:"before_shot"` // URL; may be empty
	AfterShot        string     `json:"after_shot"`
	SuiteTestID      string     `json:"suite_test_id"`
	RegressionTotal  int        `json:"regression_total"`
	RegressionPassed int        `json:"regression_passed"`
}

// Problem explains an error or an escalation.
type Problem struct {
	Stage     string `json:"stage"`   // triage | reproduce | fix | verify | regress
	Message   string `json:"message"` // what happened
	Remedy    string `json:"remedy"`  // what to do about it; may be empty
	Retryable bool   `json:"retryable"`
}

// PublicView is everything the reporter is shown. It never contains code,
// file names or tool output.
type PublicView struct {
	Stage      string `json:"stage"` // received | reproducing | question | fixing | verifying | fixed | handed_off | closed | trouble
	Step       int    `json:"step"`  // 1 to 4: received, reproduced, fix tested, fixed
	Headline   string `json:"headline"`
	Detail     string `json:"detail"`
	Question   string `json:"question"` // set when Stage is question
	BeforeShot string `json:"before_shot"`
	AfterShot  string `json:"after_shot"`
}

// PublicReport is the response of the public report endpoints and the payload
// of the public event stream.
type PublicReport struct {
	ID        string     `json:"id"`
	Seq       int        `json:"seq"`
	Text      string     `json:"text"`
	Thread    []Message  `json:"thread"`
	View      PublicView `json:"view"`
	Done      bool       `json:"done"`
	Simulated bool       `json:"simulated"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// SuiteTest is a check that proved a fix once and is now re-run after every
// later patch.
type SuiteTest struct {
	ID        string       `json:"id"` // "t_1", "t_2" ...
	Title     string       `json:"title"`
	Objective string       `json:"objective"`
	StartURL  string       `json:"start_url"`
	TestFile  string       `json:"test_file"` // saved Kane test, replayed when present
	ReportID  string       `json:"report_id"`
	ReportSeq int          `json:"report_seq"`
	CreatedAt time.Time    `json:"created_at"`
	LastRun   *SuiteResult `json:"last_run,omitempty"`
}

// SuiteResult is the most recent result of one suite test.
type SuiteResult struct {
	Status    RunStatus `json:"status"`
	Reason    string    `json:"reason"`
	DurationS float64   `json:"duration_s"`
	Replayed  bool      `json:"replayed"` // true when the saved test was replayed instead of re-authored
	At        time.Time `json:"at"`
}

// Suite is the response of GET /api/suite.
type Suite struct {
	Tests   []SuiteTest `json:"tests"`
	Running bool        `json:"running"`
}

// Stats is the board's summary line.
type Stats struct {
	Reports            int     `json:"reports"`
	Proven             int     `json:"proven"`
	InProgress         int     `json:"in_progress"`
	NeedsYou           int     `json:"needs_you"`
	MedianTimeToProofS float64 `json:"median_time_to_proof_s"`
	SuiteSize          int     `json:"suite_size"`
	CreditsUsed        float64 `json:"credits_used"`
	AgentCostUSD       float64 `json:"agent_cost_usd"`
}

// System describes the tools Fixpoint depends on and what it is doing.
type System struct {
	Version       string     `json:"version"`
	Simulated     bool       `json:"simulated"` // stub runners are in use: nothing is really verified
	Kane          ToolInfo   `json:"kane"`
	Agent         ToolInfo   `json:"agent"`
	Target        TargetInfo `json:"target"`
	Queue         QueueInfo  `json:"queue"`
	MaxAttempts   int        `json:"max_attempts"`
	Autonomy      string     `json:"autonomy"` // auto | ask
	ConsoleURL    string     `json:"console_url"`
	WidgetSnippet string     `json:"widget_snippet"` // the <script> tag to paste into an app
}

// ToolInfo is the state of Kane CLI or the coding agent.
type ToolInfo struct {
	Name     string `json:"name"` // "Kane CLI", "Claude Code"
	Found    bool   `json:"found"`
	Path     string `json:"path"`
	Version  string `json:"version"`
	SignedIn string `json:"signed_in"` // yes | no | unknown
	Stub     bool   `json:"stub"`
	Problem  string `json:"problem"` // empty when ready
	Remedy   string `json:"remedy"`  // the command that fixes Problem
}

// TargetInfo is the app Fixpoint is watching.
type TargetInfo struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Dir       string `json:"dir"`
	Reachable bool   `json:"reachable"`
	Demo      bool   `json:"demo"` // the built-in demo shop
}

// QueueInfo is what the worker is doing.
type QueueInfo struct {
	Busy           bool   `json:"busy"`
	ActiveReportID string `json:"active_report_id"`
	Pending        int    `json:"pending"`
}

// Event is one entry in a report's timeline and one message on the event
// stream. Events are thin: the console re-reads the report when one arrives,
// except for run.step, which it shows directly.
type Event struct {
	Seq      int64          `json:"seq"`       // global, increasing
	ReportID string         `json:"report_id"` // empty for suite.* and system.* events
	Type     string         `json:"type"`
	Stage    string         `json:"stage"` // triage | reproduce | fix | verify | regress | "" when not tied to a stage
	Text     string         `json:"text"`  // one plain sentence for the timeline
	Data     map[string]any `json:"data"`
	At       time.Time      `json:"at"`
}

// Event types.
const (
	EvReportCreated   = "report.created"
	EvReportStatus    = "report.status"    // data: status
	EvCheckWritten    = "check.written"    // data: revision
	EvRunStarted      = "run.started"      // data: run_id, kind, attempt, title
	EvRunStep         = "run.step"         // data: run_id, n, status, remark
	EvRunFinished     = "run.finished"     // data: run_id, kind, status, reason
	EvAttemptStarted  = "attempt.started"  // data: n
	EvAttemptPatched  = "attempt.patched"  // data: n, files, additions, deletions
	EvAttemptFinished = "attempt.finished" // data: n, status
	EvReportQuestion  = "report.question"  // data: question
	EvReportReply     = "report.reply"
	EvReportProven    = "report.proven"
	EvReportEscalated = "report.escalated"
	EvReportError     = "report.error"
	EvReportRequeued  = "report.requeued"
	EvSuiteStarted    = "suite.started"
	EvSuiteTest       = "suite.test" // data: test_id, status
	EvSuiteFinished   = "suite.finished"
	EvSystemChanged   = "system.changed"
)

// NewReportRequest is the body of POST /api/public/reports.
type NewReportRequest struct {
	Text     string   `json:"text"`
	Reporter Reporter `json:"reporter"`
	Context  Context  `json:"context"`
}

// NewReportResponse is what the widget gets back.
type NewReportResponse struct {
	ID        string `json:"id"`
	Seq       int    `json:"seq"`
	StatusURL string `json:"status_url"` // the public status page
	EventsURL string `json:"events_url"` // the public event stream
}

// ReportDetail is the response of GET /api/reports/{id}.
type ReportDetail struct {
	Report Report  `json:"report"`
	Events []Event `json:"events"`
}

// ReportList is the response of GET /api/reports.
type ReportList struct {
	Reports []ReportSummary `json:"reports"`
	Stats   Stats           `json:"stats"`
}
