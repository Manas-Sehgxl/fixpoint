// Package doctor checks the tools Fixpoint depends on and says what to do
// about anything missing.
package doctor

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
)

// Check is one line of the report.
type Check struct {
	Name     string
	OK       bool
	Required bool
	Detail   string
	Remedy   string
}

// Report is everything doctor looked at.
type Report struct {
	Checks []Check
}

// Problems reports whether something required is missing.
func (r Report) Problems() bool {
	for _, c := range r.Checks {
		if c.Required && !c.OK {
			return true
		}
	}
	return false
}

// Write prints one line per check, with remedies underneath.
func (r Report) Write(w io.Writer) {
	for _, c := range r.Checks {
		mark := "ok  "
		if !c.OK {
			mark = "no  "
			if !c.Required {
				mark = "--  "
			}
		}
		fmt.Fprintf(w, "%s%-12s %s\n", mark, c.Name, c.Detail)
		if !c.OK && c.Remedy != "" {
			fmt.Fprintf(w, "              %s\n", c.Remedy)
		}
	}
}

// FromTool turns a ToolInfo into a check.
func FromTool(t core.ToolInfo, required bool) Check {
	c := Check{Name: t.Name, Required: required, Remedy: t.Remedy}
	switch {
	case t.Problem != "":
		c.Detail = t.Problem
		if t.Found && t.Version != "" {
			c.Detail = fmt.Sprintf("%s (%s at %s)", t.Problem, t.Version, t.Path)
		}
	case t.Stub:
		c.OK = true
		c.Detail = "simulated runner (development only)"
	default:
		c.OK = true
		c.Detail = fmt.Sprintf("%s at %s", t.Version, t.Path)
		if t.SignedIn == "yes" {
			c.Detail += ", signed in"
		}
	}
	return c
}

// Chrome checks that a browser Kane can drive is installed.
func Chrome(ctx context.Context) Check {
	c := Check{Name: "Chrome", Required: false,
		Remedy: "Install Google Chrome; Kane CLI drives it. Kane can also use its own managed browser."}
	for _, bin := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(bin); err == nil {
			c.OK = true
			c.Detail = p
			return c
		}
	}
	c.Detail = "not found on PATH"
	return c
}

// Target checks that the app under repair answers.
func Target(ctx context.Context, t core.TargetInfo) Check {
	c := Check{Name: "Target", Required: true}
	if t.Demo {
		c.OK = true
		c.Detail = fmt.Sprintf("the built-in demo shop, served by `fixpoint up` at %s", t.URL)
		return c
	}
	if t.URL == "" {
		c.Detail = "no target.url is set, so Fixpoint would run its own demo shop"
		c.OK = true
		return c
	}
	c.Detail = fmt.Sprintf("%s at %s", t.Name, t.URL)
	if !t.Reachable {
		c.Detail = fmt.Sprintf("%s did not answer", t.URL)
		c.Remedy = "Start the app, or correct target.url in fixpoint.json."
		return c
	}
	if t.Dir == "" {
		c.Detail = fmt.Sprintf("%s answers, but target.dir is empty", t.URL)
		c.Remedy = "Set target.dir to the app's source directory so the agent can patch it."
		return c
	}
	c.OK = true
	c.Detail += fmt.Sprintf(", source in %s", t.Dir)
	return c
}

// Reachable reports whether url answers within a second or two.
func Reachable(ctx context.Context, url string) bool {
	if url == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}
