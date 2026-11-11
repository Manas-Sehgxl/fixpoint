// Package proc runs child processes in their own process group so that a
// cancel or a deadline stops the child and everything it started (Kane
// launches Chrome, Claude Code launches tools).
package proc

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

// Spec describes one child process.
type Spec struct {
	Bin    string
	Args   []string
	Dir    string
	Env    []string // full environment; nil inherits the parent's
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// ErrNotFound means the binary could not be found or started.
var ErrNotFound = errors.New("binary not found")

// Run starts the process, waits for it and returns its exit code. When ctx
// ends first, the whole process group is killed and the context's error is
// returned with exit code -1. A binary that cannot be started returns an
// error wrapping ErrNotFound.
func Run(ctx context.Context, s Spec) (int, error) {
	cmd := exec.CommandContext(ctx, s.Bin, s.Args...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	cmd.Stdin = s.Stdin
	cmd.Stdout = s.Stdout
	cmd.Stderr = s.Stderr
	setGroup(cmd)
	cmd.Cancel = func() error { return killGroup(cmd) }
	// Grandchildren may keep the output pipes open after the child exits;
	// stop waiting for them after a short grace period.
	cmd.WaitDelay = 3 * time.Second
	if err := cmd.Start(); err != nil {
		return -1, errors.Join(ErrNotFound, err)
	}
	err := cmd.Wait()
	_ = killGroup(cmd) // clean up anything the child left running
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return -1, err
	}
	return cmd.ProcessState.ExitCode(), nil
}

// Shell returns the binary and arguments that run a command line through the
// platform's shell.
func Shell(line string) (string, []string) { return shell(line) }
