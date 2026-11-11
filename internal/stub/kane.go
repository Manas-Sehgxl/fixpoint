package stub

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// KaneVersion is what `stub-kane --version` prints.
const KaneVersion = "stub-kane 0.0.0"

const runHelp = `Usage: kane-cli run <objective> [options]

Options:
  --agent                 stream NDJSON events on stdout
  --headless              run the browser without a window
  --url <url>             the page to start on
  --timeout <seconds>     give up after this long
  --max-steps <n>         stop after this many steps
  --name <slug>           save the run as a replayable test
  --bug-detection <mode>  continue | stop: flag confirmed product bugs
`

// KaneMain is the entry point of the hidden `fixpoint stub-kane` command. It
// takes the arguments kane-cli takes and returns kane-cli's exit code.
func KaneMain(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, runHelp)
		return 2
	}
	switch args[0] {
	case "--version", "-v":
		fmt.Println(KaneVersion)
		return 0
	case "whoami":
		fmt.Println("stub-kane: signed in as the simulated runner")
		return 0
	case "testmd":
		if len(args) >= 2 && args[1] == "run" {
			return stubTestMD(args[2:])
		}
		fmt.Fprintln(os.Stderr, "stub-kane: only `testmd run` is implemented")
		return 2
	case "run":
		if contains(args, "--help") || contains(args, "-h") {
			fmt.Print(runHelp)
			return 0
		}
		return stubRun(args[1:])
	}
	fmt.Fprintf(os.Stderr, "stub-kane: unknown command %q\n", args[0])
	return 2
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// flag returns the value of --name style flags.
func flag(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, name+"=") {
			return strings.TrimPrefix(a, name+"=")
		}
	}
	return ""
}

// positional returns the first argument that is not a flag or a flag value.
func positional(args []string) string {
	valued := map[string]bool{"--url": true, "--timeout": true, "--max-steps": true, "--name": true, "--bug-detection": true, "--model": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if valued[a] {
				i++
			}
			continue
		}
		return a
	}
	return ""
}

func emit(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

func stepMS() time.Duration {
	ms := 600
	if v := os.Getenv("STUB_STEP_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			ms = n
		}
	}
	return time.Duration(ms) * time.Millisecond
}

var sentenceEnd = regexp.MustCompile(`[.!?]+(\s|$)`)

// steps splits an objective into the steps the stub pretends to take: one
// per sentence.
func steps(objective string) []string {
	var out []string
	rest := strings.TrimSpace(objective)
	for rest != "" {
		m := sentenceEnd.FindStringIndex(rest)
		if m == nil {
			out = append(out, rest)
			break
		}
		out = append(out, strings.TrimSpace(rest[:m[1]]))
		rest = strings.TrimSpace(rest[m[1]:])
	}
	var kept []string
	for _, s := range out {
		if strings.TrimSpace(strings.Trim(s, ".!? ")) != "" {
			kept = append(kept, s)
		}
	}
	if len(kept) == 0 {
		kept = []string{"Run the check"}
	}
	return kept
}

func stubRun(args []string) int {
	objective := positional(args)
	url := flag(args, "--url")
	name := flag(args, "--name")
	scs := LoadScenarios(os.Getenv("STUB_SCENARIOS"))
	targetDir := os.Getenv("STUB_TARGET_DIR")
	cwd, _ := os.Getwd()

	emit(map[string]any{"type": "stream_start", "cli_version": "0.10.0", "surface": "run",
		"pid": os.Getpid(), "v": 1, "ts": time.Now().UTC().Format(time.RFC3339)})

	if strings.Contains(objective, "[stub:error]") {
		emit(map[string]any{"type": "error", "message": "Simulated start-up failure: not signed in."})
		fmt.Fprintln(os.Stderr, "stub-kane: simulated error before the run")
		return 2
	}

	sc := scs.MatchObjective(objective, url)
	fail := sc != nil && sc.StillBuggy(targetDir)
	list := steps(objective)
	pause := stepMS()
	start := time.Now()
	for i, s := range list {
		n := i + 1
		emit(map[string]any{"step": n, "status": "running", "remark": s})
		if pause > 0 {
			time.Sleep(pause)
		}
		status := "done"
		remark := s
		if fail && i == len(list)-1 {
			status = "failed"
			remark = sc.Observed
			if remark == "" {
				remark = "The check did not pass."
			}
		}
		emit(map[string]any{"step": n, "status": status, "remark": remark})
	}
	if strings.Contains(objective, "[stub:timeout]") {
		fmt.Fprintln(os.Stderr, "stub-kane: simulated timeout")
		return 3
	}

	sessionDir := filepath.Join(cwd, ".testmuai", "sessions", fmt.Sprintf("stub-%d", time.Now().UnixNano()))
	pack, perr := writePack(cwd, sessionDir, url, list, fail)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "stub-kane: could not write the evidence pack: %v\n", perr)
	}
	if name != "" {
		if err := writeTestMD(cwd, name, objective, url); err != nil {
			fmt.Fprintf(os.Stderr, "stub-kane: could not save the test: %v\n", err)
		}
	}

	end := map[string]any{
		"type": "run_end", "duration": time.Since(start).Seconds(), "credits_consumed": 2.5 + float64(len(list))*0.8,
		"session_dir": sessionDir, "test_url": "", "final_state": map[string]any{},
	}
	if fail {
		observed := sc.Observed
		end["status"] = "failed"
		end["summary"] = observed
		end["one_liner"] = sc.Title
		end["reason"] = fmt.Sprintf("assertion_failed: @ step %d", len(list))
		end["final_state"] = map[string]any{"observed": observed}
		end["result_code"] = "740"
		end["verdict"] = map[string]any{"confirmed": true, "family": "simulated", "category": sc.ID,
			"severity": "high", "one_liner": sc.Title, "confidence": "high"}
	} else {
		end["status"] = "passed"
		end["summary"] = "Every step passed and the final assertion held."
		end["one_liner"] = "The check passed"
		end["reason"] = ""
		end["final_state"] = map[string]any{"observed": "as expected"}
	}
	emit(end)
	if pack != "" {
		fmt.Fprintf(os.Stderr, "evidence: view locally with `kane-cli evidence serve %s`\n", pack)
	}
	if fail {
		return 1
	}
	return 0
}

// writeTestMD records the run as a replayable test, as --name does.
func writeTestMD(cwd, name, objective, url string) error {
	dir := filepath.Join(cwd, ".testmuai", "tests")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf("# %s\n\nStart URL: %s\n\n## Step 1: %s\n\n%s\n", name, url, name, objective)
	return os.WriteFile(filepath.Join(dir, name+"_test.md"), []byte(body), 0o644)
}

// readTestMD reads an objective back out of a saved test.
func readTestMD(path string) (objective, url string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	text := string(b)
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Start URL: ") {
			url = strings.TrimSpace(strings.TrimPrefix(line, "Start URL: "))
		}
	}
	if i := strings.LastIndex(text, "\n\n"); i >= 0 {
		objective = strings.TrimSpace(text[i:])
	}
	if objective == "" {
		objective = strings.TrimSpace(text)
	}
	return objective, url
}

func stubTestMD(args []string) int {
	path := positional(args)
	objective, url := readTestMD(path)
	scs := LoadScenarios(os.Getenv("STUB_SCENARIOS"))
	targetDir := os.Getenv("STUB_TARGET_DIR")
	sc := scs.MatchObjective(objective, url)
	fail := sc != nil && sc.StillBuggy(targetDir)
	heading := strings.TrimSuffix(filepath.Base(path), "_test.md")
	start := time.Now()

	emit(map[string]any{"type": "stream_start", "cli_version": "0.10.0", "surface": "testmd",
		"pid": os.Getpid(), "v": 1, "ts": time.Now().UTC().Format(time.RFC3339)})
	emit(map[string]any{"type": "test_md_step_start", "step_index": 0, "heading": heading})
	list := steps(objective)
	pause := stepMS()
	for i, s := range list {
		emit(map[string]any{"step": i + 1, "status": "running", "remark": s})
		if pause > 0 {
			time.Sleep(pause)
		}
		status := "done"
		if fail && i == len(list)-1 {
			status = "failed"
		}
		emit(map[string]any{"step": i + 1, "status": status, "remark": s})
	}
	status := "passed"
	end := map[string]any{"type": "run_end", "status": status, "summary": "Replayed from the recording.",
		"reason": "", "duration": time.Since(start).Seconds(), "credits_consumed": 1.2,
		"final_state": map[string]any{"observed": "as expected"}}
	if fail {
		status = "failed"
		end["status"] = "failed"
		end["summary"] = sc.Observed
		end["reason"] = fmt.Sprintf("assertion_failed: @ step %d", len(list))
		end["final_state"] = map[string]any{"observed": sc.Observed}
	}
	emit(end)
	emit(map[string]any{"type": "test_md_step_end", "step_index": 0, "status": status})
	passed, failed := 1, 0
	if fail {
		passed, failed = 0, 1
	}
	emit(map[string]any{"type": "test_md_summary", "overall_status": status, "duration_s": time.Since(start).Seconds(),
		"steps": map[string]any{"total": 1, "passed": passed, "failed": failed, "skipped": 0,
			"replay_decisions": len(list), "author_decisions": 0}})
	emit(map[string]any{"type": "test_md_done", "overall_status": status, "duration_s": time.Since(start).Seconds(),
		"session_id": fmt.Sprintf("stub-%d", time.Now().Unix()), "share_url": ""})
	if fail {
		return 1
	}
	return 0
}

// writePack writes an evidence pack zip with one screenshot per step, in the
// layout the real packs use, and returns its path.
func writePack(cwd, sessionDir, url string, list []string, fail bool) (string, error) {
	shot := screenshot(url)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, body []byte) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write(body)
		return err
	}
	testID := "stub-test"
	status := "passed"
	if fail {
		status = "failed"
	}
	if err := add("run.yaml", []byte(fmt.Sprintf("simulated: true\nstatus: %s\nurl: %s\n", status, url))); err != nil {
		return "", err
	}
	if err := add("tests/"+testID+"/result.yaml", []byte("status: "+status+"\n")); err != nil {
		return "", err
	}
	if err := add("tests/"+testID+"/logs/1-console.ndjson", []byte(`{"level":"info","text":"simulated run"}`+"\n")); err != nil {
		return "", err
	}
	for i := range list {
		base := fmt.Sprintf("tests/%s/steps/%d-step/", testID, i+1)
		if err := add(base+"screenshot.png", shot); err != nil {
			return "", err
		}
		if err := add(base+"annotated.png", shot); err != nil {
			return "", err
		}
		if fail && i == len(list)-1 {
			if err := add(base+"failure.yaml", []byte("kind: assertion_failed\nstep: "+strconv.Itoa(i+1)+"\nnote: simulated failure\n")); err != nil {
				return "", err
			}
		}
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	name := fmt.Sprintf("stub-%d.evidence", time.Now().UnixNano())
	for _, dir := range []string{filepath.Join(cwd, ".testmuai", "evidence"), filepath.Join(sessionDir, "evidence")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0o644); err != nil {
			return "", err
		}
	}
	return filepath.Join(cwd, ".testmuai", "evidence", name), nil
}

// screenshot returns a PNG of the page when STUB_CHROME=1 and Chrome is
// installed, and a small generated PNG otherwise.
func screenshot(url string) []byte {
	if os.Getenv("STUB_CHROME") == "1" && url != "" {
		if bin, err := exec.LookPath("google-chrome"); err == nil {
			dir, err := os.MkdirTemp("", "stub-shot")
			if err == nil {
				defer os.RemoveAll(dir)
				out := filepath.Join(dir, "shot.png")
				cmd := exec.Command(bin, "--headless=new", "--no-sandbox", "--disable-gpu", "--hide-scrollbars",
					"--window-size=1200,800", "--screenshot="+out, url)
				cmd.Dir = dir
				if cmd.Run() == nil {
					if b, err := os.ReadFile(out); err == nil && len(b) > 0 {
						return b
					}
				}
			}
		}
	}
	return generatedPNG()
}

// generatedPNG draws the paper-and-grid placeholder used when no browser is
// available, so the console always has something to show.
func generatedPNG() []byte {
	const w, h = 320, 200
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	paper := color.RGBA{0xF5, 0xF7, 0xFA, 0xFF}
	grid := color.RGBA{0xDD, 0xE5, 0xF0, 0xFF}
	ink := color.RGBA{0x27, 0x43, 0xD2, 0xFF}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := paper
			if x%20 == 0 || y%20 == 0 {
				c = grid
			}
			if y > h-24 && x < 120 {
				c = ink
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	return buf.Bytes()
}
