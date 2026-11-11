package stub

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// AgentVersion is what `stub-agent --version` prints.
const AgentVersion = "stub-agent 0.0.0 (Claude Code stand-in)"

// The default question when a report says too little to act on.
const askQuestion = "What were you doing when it went wrong, and what did you expect to happen?"

// AgentMain is the entry point of the hidden `fixpoint stub-agent` command.
// It takes the arguments Claude Code takes, reads the prompt from stdin and
// prints a Claude-style JSON envelope.
func AgentMain(args []string) int {
	if contains(args, "--version") {
		fmt.Println(AgentVersion)
		return 0
	}
	prompt, err := io.ReadAll(os.Stdin)
	if err != nil {
		return envelope(nil, "the prompt could not be read", 0)
	}
	wait := 2000
	if v := os.Getenv("STUB_AGENT_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			wait = n
		}
	}
	start := time.Now()
	time.Sleep(time.Duration(wait) * time.Millisecond)
	text := string(prompt)
	scs := LoadScenarios(os.Getenv("STUB_SCENARIOS"))
	dir := os.Getenv("STUB_TARGET_DIR")
	first := strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	switch {
	case strings.Contains(first, "triage"):
		return envelope(stubTriage(scs, text), "", time.Since(start))
	case strings.Contains(first, "fix"):
		out, err := stubFix(scs, dir, text)
		if err != "" {
			return envelope(nil, err, time.Since(start))
		}
		return envelope(out, "", time.Since(start))
	}
	return envelope(nil, "the prompt did not name a Fixpoint task", time.Since(start))
}

func envelope(out any, errMsg string, dur time.Duration) int {
	env := map[string]any{
		"type": "result", "subtype": "success", "is_error": errMsg != "",
		"session_id":  fmt.Sprintf("stub-%d", time.Now().UnixNano()),
		"duration_ms": dur.Milliseconds(), "num_turns": 2, "total_cost_usd": 0.0123,
	}
	if errMsg != "" {
		env["result"] = "stub-agent: " + errMsg
	} else {
		b, _ := json.Marshal(out)
		env["result"] = string(b)
		env["structured_output"] = out
	}
	b, _ := json.Marshal(env)
	fmt.Println(string(b))
	if errMsg != "" {
		return 1
	}
	return 0
}

var reportBlock = regexp.MustCompile(`(?si)<report>(.*?)</report>`)
var targetURL = regexp.MustCompile(`live app at (\S+?)[.,\s]`)

func between(text, start, end string) string {
	i := strings.Index(text, start)
	if i < 0 {
		return ""
	}
	rest := text[i+len(start):]
	if end == "" {
		return rest
	}
	j := strings.Index(rest, end)
	if j < 0 {
		return rest
	}
	return rest[:j]
}

func reportText(prompt string) string {
	if m := reportBlock.FindStringSubmatch(prompt); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func promptURL(prompt string) string {
	if m := targetURL.FindStringSubmatch(prompt); m != nil {
		return strings.TrimRight(m[1], ".")
	}
	return ""
}

// stubTriage answers triage: the first scenario whose keywords all appear in
// the report wins; a report with nothing to go on gets a question.
func stubTriage(scs Scenarios, prompt string) map[string]any {
	report := reportText(prompt)
	url := promptURL(prompt)
	// Triage sees the thread, so a reporter's answer can be what makes a
	// scenario match.
	withThread := report + "\n" + between(prompt, "Earlier questions and answers:\n", "\nNotes about this app")
	if sc := scs.MatchReport(withThread); sc != nil {
		return map[string]any{"decision": "check", "title": sc.Title,
			"objective": withMarkers(Resolve(sc.Objective, url), withThread), "expected": sc.Expected, "question": sc.Question}
	}
	if len(strings.Fields(withThread)) < 5 {
		return map[string]any{"decision": "ask", "title": firstWords(report, 70),
			"objective": "", "expected": "", "question": askQuestion}
	}
	return map[string]any{"decision": "check", "title": firstWords(report, 70),
		"objective": withMarkers(fmt.Sprintf("Go to %s. Verify the page shows the menu.", strings.TrimRight(url, "/")), withThread),
		"expected":  "The page loads and shows the menu.",
		"question":  "Could you tell us the exact steps you took, and what you saw instead?"}
}

// withMarkers carries a [stub:error] or [stub:timeout] marker from the
// report into the check, which is how the stub Kane is told to fail before
// the run: the markers only reach it through the objective.
func withMarkers(objective, report string) string {
	for _, m := range []string{"[stub:error]", "[stub:timeout]"} {
		if strings.Contains(report, m) && !strings.Contains(objective, m) {
			objective += " " + m
		}
	}
	return objective
}

func firstWords(s string, n int) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if s == "" {
		return "A report with no details"
	}
	if len(s) > n {
		s = strings.TrimRight(s[:n], " ,.;:")
	}
	return s
}

// stubFix answers a fix attempt: it finds the scenario from the check in the
// prompt and really replaces the buggy text with the fixed text.
func stubFix(scs Scenarios, dir, prompt string) (map[string]any, string) {
	objective := strings.TrimSpace(between(prompt, "## The check Kane runs\n", "\nCorrect behaviour:"))
	if i := strings.Index(objective, "\n"); i >= 0 && strings.Contains(objective[i:], "Go to ") {
		objective = strings.TrimSpace(objective)
	}
	report := reportText(prompt)
	sc := scs.MatchObjective(objective, promptURL(prompt))
	if strings.Contains(report, "[stub:giveup]") {
		cause := "The simulated agent was told to give up by the report."
		if sc != nil {
			cause = "The simulated agent was told to give up: " + sc.RootCause
		}
		return map[string]any{"action": "give_up", "root_cause": cause,
			"summary": "Nothing was changed.", "revised_objective": "", "revised_expected": ""}, ""
	}
	if sc == nil {
		return nil, "no scenario matches this check, so the simulated agent has nothing to patch"
	}
	path := scenarioPath(dir, sc.File)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Sprintf("could not read %s: %v", sc.File, err)
	}
	body := string(b)
	if strings.Contains(report, "[stub:stubborn]") {
		// A harmless wrong edit: the check keeps failing, so the report
		// escalates and the rollback can be seen.
		stamp := fmt.Sprintf("\n/* fixpoint stub attempt %d */\n", time.Now().UnixNano()%1000)
		if err := os.WriteFile(path, []byte(body+stamp), 0o644); err != nil {
			return nil, fmt.Sprintf("could not write %s: %v", sc.File, err)
		}
		return map[string]any{"action": "patched",
			"root_cause":        "The simulated agent was told to keep missing the real cause.",
			"summary":           "Added a note near the suspect code.",
			"revised_objective": "", "revised_expected": ""}, ""
	}
	if !strings.Contains(body, sc.Buggy) {
		return map[string]any{"action": "give_up",
			"root_cause":        fmt.Sprintf("%s no longer contains the code this scenario describes, so the simulated agent cannot patch it.", sc.File),
			"summary":           "Nothing was changed.",
			"revised_objective": "", "revised_expected": ""}, ""
	}
	if err := os.WriteFile(path, []byte(strings.Replace(body, sc.Buggy, sc.Fixed, 1)), 0o644); err != nil {
		return nil, fmt.Sprintf("could not write %s: %v", sc.File, err)
	}
	return map[string]any{"action": "patched", "root_cause": sc.RootCause,
		"summary":           fmt.Sprintf("Changed %s so that %s", sc.File, strings.ToLower(strings.TrimRight(sc.Expected, "."))+"."),
		"revised_objective": "", "revised_expected": ""}, ""
}
