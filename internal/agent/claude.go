package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
	"github.com/Manas-Sehgxl/fixpoint/internal/proc"
)

// RemedyInstall is shown when the agent binary is missing.
const RemedyInstall = "Install Claude Code and sign in, or set `agent.bin` in fixpoint.json."

// Claude runs Claude Code non-interactively (`claude -p`).
type Claude struct {
	Bin        string
	PrefixArgs []string // e.g. ["stub-agent"] for the stub
	Dir        string   // the target directory
	Model      string
	Timeout    time.Duration
	Env        []string
	Simulated  bool
}

// Name is the agent's display name.
func (c *Claude) Name() string { return "Claude Code" }

// Envelope is Claude Code's JSON output.
type Envelope struct {
	Result           string          `json:"result"`
	StructuredOutput json.RawMessage `json:"structured_output"`
	IsError          bool            `json:"is_error"`
	SessionID        string          `json:"session_id"`
	TotalCostUSD     float64         `json:"total_cost_usd"`
	DurationMS       int64           `json:"duration_ms"`
	NumTurns         int             `json:"num_turns"`
}

// ParseOutput reads Claude Code's output into out (a pointer to Triage or
// Fix). It uses structured_output when present, otherwise the first JSON
// object inside result.
func ParseOutput(stdout []byte, out any) (Envelope, error) {
	var env Envelope
	b := bytes.TrimSpace(stdout)
	// Some versions print other lines first; the envelope is the last
	// JSON object on its own line.
	if i := bytes.LastIndex(b, []byte("\n{")); i >= 0 && json.Unmarshal(b, &env) != nil {
		b = b[i+1:]
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return env, fmt.Errorf("the agent's output is not JSON (%v)", err)
	}
	if env.IsError {
		return env, fmt.Errorf("the agent reported an error: %s", clip(env.Result, 300))
	}
	if len(env.StructuredOutput) > 0 && string(env.StructuredOutput) != "null" {
		if err := json.Unmarshal(env.StructuredOutput, out); err == nil {
			return env, nil
		}
	}
	if obj := firstObject(env.Result); obj != "" {
		if err := json.Unmarshal([]byte(obj), out); err == nil {
			return env, nil
		}
	}
	return env, errors.New("the agent's answer had no usable JSON object")
}

// firstObject returns the first balanced JSON object in s.
func firstObject(s string) string {
	for start := strings.IndexByte(s, '{'); start >= 0; {
		depth, inStr, esc := 0, false, false
		for i := start; i < len(s); i++ {
			ch := s[i]
			switch {
			case esc:
				esc = false
			case inStr && ch == '\\':
				esc = true
			case ch == '"':
				inStr = !inStr
			case inStr:
			case ch == '{':
				depth++
			case ch == '}':
				depth--
				if depth == 0 {
					cand := s[start : i+1]
					if json.Valid([]byte(cand)) {
						return cand
					}
					i = len(s)
				}
			}
		}
		next := strings.IndexByte(s[start+1:], '{')
		if next < 0 {
			return ""
		}
		start += next + 1
	}
	return ""
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func (c *Claude) call(ctx context.Context, prompt, schema string, tools string, edit bool, out any) (core.AgentCall, error) {
	call := core.AgentCall{Name: c.Name()}
	args := append([]string{}, c.PrefixArgs...)
	args = append(args, "-p", "--output-format", "json", "--json-schema", schema)
	if edit {
		args = append(args, "--permission-mode", "acceptEdits")
	}
	args = append(args, "--allowedTools", tools)
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx2, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	t0 := time.Now()
	code, err := proc.Run(ctx2, proc.Spec{Bin: c.Bin, Args: args, Dir: c.Dir,
		Env: append(append(os.Environ(), "FIXPOINT_AGENT=1"), c.Env...), Stdin: strings.NewReader(prompt),
		Stdout: &stdout, Stderr: &stderr})
	call.DurationMS = time.Since(t0).Milliseconds()
	switch {
	case errors.Is(err, proc.ErrNotFound):
		return call, errors.New("the coding agent could not be started. " + RemedyInstall)
	case ctx.Err() != nil:
		return call, ctx.Err()
	case ctx2.Err() != nil:
		return call, fmt.Errorf("the coding agent did not finish within %s; raise agent.timeout_s in fixpoint.json", timeout)
	case err != nil:
		return call, fmt.Errorf("the coding agent could not be run: %w", err)
	}
	env, perr := ParseOutput(stdout.Bytes(), out)
	call.SessionID, call.CostUSD, call.Turns = env.SessionID, env.TotalCostUSD, env.NumTurns
	if env.DurationMS > 0 {
		call.DurationMS = env.DurationMS
	}
	if code != 0 {
		msg := clip(stderr.String(), 300)
		if msg == "" {
			msg = clip(env.Result, 300)
		}
		return call, fmt.Errorf("the coding agent exited with code %d: %s", code, orNone(msg))
	}
	return call, perr
}

// Triage asks the agent to write the check, read-only.
func (c *Claude) Triage(ctx context.Context, in TriageInput) (Triage, core.AgentCall, error) {
	var t Triage
	call, err := c.call(ctx, TriagePrompt(in), TriageSchema, "Read,Glob,Grep", false, &t)
	if err == nil && t.Decision != "check" && t.Decision != "ask" {
		err = fmt.Errorf("the agent answered decision %q, which is neither check nor ask", t.Decision)
	}
	if err == nil && t.Decision == "check" && strings.TrimSpace(t.Objective) == "" {
		err = errors.New("the agent chose to write a check but left it empty")
	}
	return t, call, err
}

// Fix asks the agent to patch the target directory.
func (c *Claude) Fix(ctx context.Context, in FixInput) (Fix, core.AgentCall, error) {
	var f Fix
	call, err := c.call(ctx, FixPrompt(in), FixSchema, "Read,Edit,Write,Glob,Grep", true, &f)
	if err == nil {
		switch f.Action {
		case "patched", "revise_check", "give_up":
		default:
			err = fmt.Errorf("the agent answered action %q, which is not patched, revise_check or give_up", f.Action)
		}
	}
	return f, call, err
}

// Health reports whether the agent binary is installed.
func (c *Claude) Health(ctx context.Context) core.ToolInfo {
	info := core.ToolInfo{Name: c.Name(), SignedIn: "unknown", Stub: c.Simulated}
	p, err := exec.LookPath(c.Bin)
	if err != nil {
		info.Problem = "Claude Code is not installed."
		info.Remedy = RemedyInstall
		return info
	}
	info.Found, info.Path = true, p
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var out bytes.Buffer
	code, err := proc.Run(ctx, proc.Spec{Bin: p, Args: append(append([]string{}, c.PrefixArgs...), "--version"),
		Stdout: &out, Stderr: &out})
	if err != nil || code != 0 {
		info.Problem = "Claude Code is installed but `claude --version` failed."
		info.Remedy = RemedyInstall
		return info
	}
	info.Version = strings.TrimSpace(strings.SplitN(strings.TrimSpace(out.String()), "\n", 2)[0])
	if c.Simulated {
		info.SignedIn = "yes"
	}
	return info
}
