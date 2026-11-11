// Package stub holds the stand-ins for Kane CLI and the coding agent. They
// exist so Fixpoint can be developed and tested where a real browser and a
// real model cannot run. They are used only when `fixpoint up --stubs` is
// passed, and every surface then says that runs are simulated.
package stub

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Scenario is one planted bug the stubs know about, from web/dev/scenarios.json.
type Scenario struct {
	ID             string   `json:"id"`
	ReportKeywords []string `json:"report_keywords"`
	Title          string   `json:"title"`
	Objective      string   `json:"objective"`
	Expected       string   `json:"expected"`
	Question       string   `json:"question"`
	File           string   `json:"file"`
	Buggy          string   `json:"buggy"`
	Fixed          string   `json:"fixed"`
	Observed       string   `json:"observed"`
	RootCause      string   `json:"root_cause"`
}

// Scenarios is the whole scenarios file.
type Scenarios struct {
	Scenarios []Scenario `json:"scenarios"`
}

// LoadScenarios reads the scenarios file named by STUB_SCENARIOS. A missing
// path is not an error: the stubs then fall back to their generic answers.
func LoadScenarios(path string) Scenarios {
	var s Scenarios
	if path == "" {
		return s
	}
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stub: could not read the scenarios file %s: %v\n", path, err)
		return s
	}
	if err := json.Unmarshal(b, &s); err != nil {
		fmt.Fprintf(os.Stderr, "stub: %s is not valid JSON: %v\n", path, err)
	}
	return s
}

var spaces = regexp.MustCompile(`\s+`)

// collapse makes two objectives comparable: whitespace collapsed, trimmed.
func collapse(s string) string { return strings.TrimSpace(spaces.ReplaceAllString(s, " ")) }

// Resolve fills {{url}} in an objective with the target URL.
func Resolve(objective, url string) string {
	return strings.ReplaceAll(objective, "{{url}}", strings.TrimRight(url, "/"))
}

// MatchObjective finds the scenario whose objective is the one being run.
// The start URL may be any of the URLs the objective could have been built
// with, so every {{url}} spelling is tried.
func (s Scenarios) MatchObjective(objective, url string) *Scenario {
	want := collapse(objective)
	for i := range s.Scenarios {
		sc := &s.Scenarios[i]
		for _, u := range urlForms(url, want) {
			if collapse(Resolve(sc.Objective, u)) == want {
				return sc
			}
		}
	}
	return nil
}

// urlForms returns the URL spellings to try: the one Kane was given, the
// one in the objective's opening "Go to <url>." step, and both with and
// without a trailing slash.
func urlForms(url, objective string) []string {
	forms := []string{url}
	if m := regexp.MustCompile(`(?i)^go to (\S+?)[.,;]`).FindStringSubmatch(objective); m != nil {
		forms = append(forms, m[1], strings.TrimRight(m[1], "/"))
	}
	var out []string
	seen := map[string]bool{}
	for _, f := range forms {
		for _, v := range []string{f, strings.TrimRight(f, "/"), f + "/"} {
			if v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

// MatchReport finds the first scenario whose keywords all appear in the
// report text.
func (s Scenarios) MatchReport(text string) *Scenario {
	low := strings.ToLower(text)
	for i := range s.Scenarios {
		sc := &s.Scenarios[i]
		if len(sc.ReportKeywords) == 0 {
			continue
		}
		all := true
		for _, k := range sc.ReportKeywords {
			if !strings.Contains(low, strings.ToLower(k)) {
				all = false
				break
			}
		}
		if all {
			return sc
		}
	}
	return nil
}

// StillBuggy reports whether the scenario's file in dir still holds the
// buggy text, which is what makes the stub's run fail.
func (sc *Scenario) StillBuggy(dir string) bool {
	if dir == "" || sc.File == "" || sc.Buggy == "" {
		return false
	}
	b, err := os.ReadFile(scenarioPath(dir, sc.File))
	if err != nil {
		return false
	}
	return strings.Contains(string(b), sc.Buggy)
}

// scenarioPath resolves a scenario's slash-separated file path inside dir.
func scenarioPath(dir, rel string) string {
	return filepath.Join(dir, filepath.FromSlash(rel))
}
