package kane

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
	"github.com/Manas-Sehgxl/fixpoint/internal/proc"
)

// Remedies shown to the user, word for word from the spec.
const (
	RemedyNotStarted = "Kane CLI could not start. Check that you are signed in (`kane-cli whoami`, then `kane-cli login`) and that Chrome is installed."
	RemedyInstall    = "Install Kane CLI: `npm install -g @testmuai/kane-cli`, then `kane-cli login`."
)

// Request is one Kane run.
type Request struct {
	Objective string
	StartURL  string
	// Name records the run as a replayable test (<slug>_test.md).
	Name string
	// TestFile, when set, replays a saved test with `testmd run` instead.
	TestFile string
	// ArtifactDir receives the screenshots; ArtifactURL is its URL path.
	ArtifactDir string
	ArtifactURL string
}

// Result is a finished run plus what only the runner knows.
type Result struct {
	Run      core.Run
	TestFile string // the saved test when Name was set and Kane wrote it
	Replayed bool   // a testmd run replayed from its recording
}

// CLI runs the real Kane CLI (or the stub, which takes the same arguments).
type CLI struct {
	Bin        string
	PrefixArgs []string // e.g. ["stub-kane"] when Bin is the fixpoint binary
	WorkDir    string   // .fixpoint/kane
	Env        []string // extra environment
	TimeoutS   int
	MaxSteps   int
	Headless   bool
	// BugDetection is the --bug-detection value; it is passed only when
	// `run --help` lists the flag.
	BugDetection string
	Simulated    bool

	probeOnce sync.Once
	hasBugDet bool
}

var slugRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func (c *CLI) env() []string {
	return append(append(os.Environ(), "KANE_CLI_USER_AGENT=fixpoint"), c.Env...)
}

func (c *CLI) supportsBugDetection(ctx context.Context) bool {
	c.probeOnce.Do(func() {
		var out bytes.Buffer
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		_, err := proc.Run(ctx, proc.Spec{Bin: c.Bin, Args: append(append([]string{}, c.PrefixArgs...), "run", "--help"),
			Dir: c.WorkDir, Env: c.env(), Stdout: &out, Stderr: &out})
		c.hasBugDet = err == nil && strings.Contains(out.String(), "--bug-detection")
	})
	return c.hasBugDet
}

// Args returns the command-line arguments for a request (without the binary).
func (c *CLI) Args(ctx context.Context, req Request) []string {
	args := append([]string{}, c.PrefixArgs...)
	timeout := strconv.Itoa(c.TimeoutS)
	if req.TestFile != "" {
		args = append(args, "testmd", "run", req.TestFile, "--agent")
		if c.Headless {
			args = append(args, "--headless")
		}
		return append(args, "--timeout", timeout)
	}
	args = append(args, "run", req.Objective, "--agent")
	if c.Headless {
		args = append(args, "--headless")
	}
	args = append(args, "--url", req.StartURL, "--timeout", timeout, "--max-steps", strconv.Itoa(c.MaxSteps))
	if req.Name != "" && slugRe.MatchString(req.Name) {
		args = append(args, "--name", req.Name)
	}
	if c.BugDetection != "" && c.supportsBugDetection(ctx) {
		args = append(args, "--bug-detection", c.BugDetection)
	}
	return args
}

// Run executes Kane and returns the run. It never returns a half-filled run:
// tool problems come back as status error or timeout with an explanation.
// onStep receives the step list as it grows.
func (c *CLI) Run(ctx context.Context, req Request, onStep func([]core.Step)) Result {
	start := time.Now().UTC()
	run := core.Run{Objective: req.Objective, Status: core.RunRunning, StartedAt: start, Simulated: c.Simulated,
		Steps: []core.Step{}, FinalState: map[string]any{}}
	if err := os.MkdirAll(c.WorkDir, 0o755); err != nil {
		return Result{Run: finish(run, core.RunErrored, -1, "Could not create Kane's working directory: "+err.Error())}
	}
	p := &Parser{OnStep: onStep}
	stdout := &lineWriter{fn: p.Line}
	stderr := &tailBuffer{max: 64 << 10}
	// Our deadline is a little longer than Kane's own, so Kane normally
	// reports its timeout itself.
	deadline := time.Duration(c.TimeoutS+60) * time.Second
	rctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	code, err := proc.Run(rctx, proc.Spec{Bin: c.Bin, Args: c.Args(rctx, req), Dir: c.WorkDir, Env: c.env(),
		Stdout: stdout, Stderr: stderr})
	stdout.flush()
	p.Fill(&run)
	res := Result{}
	if p.TestMD != nil {
		res.Replayed = p.TestMD.Replayed()
	}
	switch {
	case errors.Is(err, proc.ErrNotFound):
		return Result{Run: finish(run, core.RunErrored, -1, RemedyInstall)}
	case ctx.Err() != nil:
		return Result{Run: finish(run, core.RunTimeout, -1, "The run was cancelled.")}
	case rctx.Err() != nil:
		return Result{Run: finish(run, core.RunTimeout, -1,
			fmt.Sprintf("Kane CLI did not finish within %d seconds, so Fixpoint stopped it. Raise kane.timeout_s in fixpoint.json if the check is long.", c.TimeoutS+60))}
	case err != nil:
		return Result{Run: finish(run, core.RunErrored, -1, "Kane CLI could not be run: "+err.Error())}
	}
	testmd := req.TestFile != ""
	switch {
	case code == 0:
		run = finish(run, core.RunPassed, code, "")
	case code == 1:
		run = finish(run, core.RunFailed, code, "")
		if testmd && run.Reason == "" && p.TestMD != nil && p.TestMD.OverallStatus != "" {
			run.Reason = "replay " + p.TestMD.OverallStatus
		}
	case code == 3 && !testmd:
		run = finish(run, core.RunTimeout, code, fmt.Sprintf("Kane CLI stopped the run when it reached its %d second timeout. Raise kane.timeout_s in fixpoint.json if the check needs longer.", c.TimeoutS))
	default:
		msg := RemedyNotStarted
		if len(run.Steps) > 0 || len(p.Errors) > 0 {
			msg = fmt.Sprintf("Kane CLI stopped with exit code %d.", code)
			if len(p.Errors) > 0 {
				msg = fmt.Sprintf("Kane CLI stopped with an error: %s", p.Errors[len(p.Errors)-1])
			}
			if len(run.Steps) == 0 {
				msg += " " + RemedyNotStarted
			}
		}
		run = finish(run, core.RunErrored, code, msg)
	}
	sessionDir := ""
	if p.RunEnd != nil {
		sessionDir = p.RunEnd.SessionDir
	}
	if pack := FindPack(stderr.String(), sessionDir, c.WorkDir, start); pack != "" && req.ArtifactDir != "" {
		if ev, err := ReadPack(pack, req.ArtifactDir, req.ArtifactURL); err == nil {
			run.Evidence = ev
		}
	}
	if req.Name != "" && !testmd {
		tf := filepath.Join(c.WorkDir, ".testmuai", "tests", req.Name+"_test.md")
		if isFile(tf) {
			res.TestFile = tf
		}
	}
	res.Run = run
	return res
}

func finish(run core.Run, status core.RunStatus, code int, msg string) core.Run {
	now := time.Now().UTC()
	run.Status = status
	run.ExitCode = code
	run.Error = msg
	run.EndedAt = &now
	if run.DurationS == 0 {
		run.DurationS = now.Sub(run.StartedAt).Seconds()
	}
	// Steps still marked running when the process ended did not finish.
	for i := range run.Steps {
		if run.Steps[i].Status == "running" && status != core.RunRunning {
			run.Steps[i].Status = "failed"
			if status == core.RunPassed {
				run.Steps[i].Status = "done"
			}
		}
	}
	return run
}

// Health describes whether Kane CLI is installed and signed in.
func (c *CLI) Health(ctx context.Context) core.ToolInfo {
	info := core.ToolInfo{Name: "Kane CLI", SignedIn: "unknown", Stub: c.Simulated}
	bin := c.Bin
	if p, err := exec.LookPath(c.Bin); err == nil {
		bin = p
	} else {
		info.Problem = "Kane CLI is not installed."
		info.Remedy = RemedyInstall
		return info
	}
	info.Found, info.Path = true, bin
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var out bytes.Buffer
	code, err := proc.Run(ctx, proc.Spec{Bin: bin, Args: append(append([]string{}, c.PrefixArgs...), "--version"),
		Env: c.env(), Stdout: &out, Stderr: &out})
	if err != nil || code != 0 {
		info.Problem = "Kane CLI is installed but `kane-cli --version` failed."
		info.Remedy = RemedyInstall
		return info
	}
	info.Version = firstLine(out.String())
	code, err = proc.Run(ctx, proc.Spec{Bin: bin, Args: append(append([]string{}, c.PrefixArgs...), "whoami"),
		Dir: c.WorkDir, Env: c.env(), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}})
	if err == nil && code == 0 {
		info.SignedIn = "yes"
	} else {
		info.SignedIn = "no"
		info.Problem = "Kane CLI is not signed in."
		info.Remedy = "Run `kane-cli login`."
	}
	return info
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
