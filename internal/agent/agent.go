// Package agent drives the coding agent that writes checks and patches. The
// Agent interface hides the tool; Claude is the one preset, run headless.
package agent

import (
	"context"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
)

// Agent writes a check from a report (triage) and patches the target (fix).
type Agent interface {
	Name() string
	Triage(ctx context.Context, in TriageInput) (Triage, core.AgentCall, error)
	Fix(ctx context.Context, in FixInput) (Fix, core.AgentCall, error)
	Health(ctx context.Context) core.ToolInfo
}

// TriageInput is everything triage is told.
type TriageInput struct {
	TargetURL   string
	TargetNotes string
	Report      core.Report
}

// Triage is the agent's answer to triage.
type Triage struct {
	Decision  string `json:"decision"` // check | ask
	Title     string `json:"title"`
	Objective string `json:"objective"`
	Expected  string `json:"expected"`
	Question  string `json:"question"`
}

// FixInput is everything a fix attempt is told.
type FixInput struct {
	TargetURL   string
	ReportText  string
	Check       core.Check
	Attempt     int
	MaxAttempts int
	Evidence    core.Run // the failing run the attempt answers
	Previous    []core.Attempt
}

// Fix is the agent's answer to a fix attempt.
type Fix struct {
	Action           string `json:"action"` // patched | revise_check | give_up
	RootCause        string `json:"root_cause"`
	Summary          string `json:"summary"`
	RevisedObjective string `json:"revised_objective"`
	RevisedExpected  string `json:"revised_expected"`
}

// TriageSchema is the JSON schema of Triage.
const TriageSchema = `{"type":"object","properties":{"decision":{"type":"string","enum":["check","ask"]},"title":{"type":"string"},"objective":{"type":"string"},"expected":{"type":"string"},"question":{"type":"string"}},"required":["decision","title","objective","expected","question"],"additionalProperties":false}`

// FixSchema is the JSON schema of Fix.
const FixSchema = `{"type":"object","properties":{"action":{"type":"string","enum":["patched","revise_check","give_up"]},"root_cause":{"type":"string"},"summary":{"type":"string"},"revised_objective":{"type":"string"},"revised_expected":{"type":"string"}},"required":["action","root_cause","summary","revised_objective","revised_expected"],"additionalProperties":false}`
