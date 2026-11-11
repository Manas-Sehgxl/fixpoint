package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/agent"
	"github.com/Manas-Sehgxl/fixpoint/internal/core"
	"github.com/Manas-Sehgxl/fixpoint/internal/events"
	"github.com/Manas-Sehgxl/fixpoint/internal/kane"
	"github.com/Manas-Sehgxl/fixpoint/internal/pipeline"
	"github.com/Manas-Sehgxl/fixpoint/internal/store"
)

type stillFails struct{}

func (stillFails) Run(ctx context.Context, req kane.Request, onStep func([]core.Step)) kane.Result {
	now := time.Now().UTC()
	return kane.Result{Run: core.Run{Status: core.RunFailed, ExitCode: 1, Reason: "assertion_failed: @ step 2",
		Summary: "The total still shows ₹498.", Steps: []core.Step{{N: 1, Status: "failed", Remark: "Verify"}}, EndedAt: &now}}
}

type askingAgent struct{}

func (askingAgent) Name() string { return "Fake agent" }
func (askingAgent) Health(context.Context) core.ToolInfo {
	return core.ToolInfo{Name: "Fake agent", Found: true}
}
func (askingAgent) Triage(context.Context, agent.TriageInput) (agent.Triage, core.AgentCall, error) {
	return agent.Triage{Decision: "ask", Title: "Unclear", Question: "Which item?"}, core.AgentCall{}, nil
}
func (askingAgent) Fix(context.Context, agent.FixInput) (agent.Fix, core.AgentCall, error) {
	return agent.Fix{Action: "give_up", RootCause: "n/a"}, core.AgentCall{}, nil
}

type rig struct {
	t     *testing.T
	s     *Server
	st    *store.Store
	w     *pipeline.Worker
	root  string
	reset int
}

func newRig(t *testing.T) *rig {
	t.Helper()
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	bus := events.New(st)
	target := filepath.Join(root, "app")
	os.MkdirAll(target, 0o755)
	os.WriteFile(filepath.Join(target, "app.js"), []byte("x\n"), 0o644)
	w := pipeline.New(pipeline.Options{Store: st, Bus: bus, Runner: stillFails{}, Agent: askingAgent{},
		Target: pipeline.Target{URL: "http://app.test", Dir: target}, MaxAttempts: 3, MaxInfoRounds: 2,
		ArtifactsDir: filepath.Join(root, "artifacts"), SnapshotsDir: filepath.Join(root, "snapshots")})
	r := &rig{t: t, st: st, w: w, root: root}
	console := fstest.MapFS{
		"index.html":  &fstest.MapFile{Data: []byte("<title>Fixpoint console</title>")},
		"status.html": &fstest.MapFile{Data: []byte("<title>Fixpoint status</title>")},
		"app.js":      &fstest.MapFile{Data: []byte("export const a = 1;")},
	}
	widget := fstest.MapFS{"widget.js": &fstest.MapFile{Data: []byte("// widget")}}
	r.s = New(Deps{Store: st, Bus: bus, Worker: w, ArtifactsDir: filepath.Join(root, "artifacts"),
		Console: console, Widget: widget,
		System: func() core.System {
			return core.System{Version: "test", ConsoleURL: "http://localhost:7777", Queue: w.Queue(), MaxAttempts: 3}
		},
		ResetDemo: func() error { r.reset++; return nil }})
	return r
}

func (r *rig) do(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	r.t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Host = "localhost:7777"
	req.RemoteAddr = "127.0.0.1:50000"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.s.ServeHTTP(w, req)
	return w
}

var fx = map[string]string{"X-Fixpoint": "1"}

func TestConsoleAPIGuards(t *testing.T) {
	r := newRig(t)
	// Non-loopback Host is refused, on reads too.
	req := httptest.NewRequest("GET", "/api/reports", nil)
	req.Host = "fixpoint.example.com"
	w := httptest.NewRecorder()
	r.s.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "only on this machine") {
		t.Errorf("remote host: %d %s", w.Code, w.Body)
	}
	for _, host := range []string{"localhost:7777", "127.0.0.1:7777", "[::1]:7777"} {
		req = httptest.NewRequest("GET", "/api/system", nil)
		req.Host = host
		w = httptest.NewRecorder()
		r.s.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("host %s: %d", host, w.Code)
		}
	}
	// A state-changing request needs the header.
	if w := r.do("POST", "/api/suite/run", "", nil); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "X-Fixpoint: 1") {
		t.Errorf("missing header: %d %s", w.Code, w.Body)
	}
	if w := r.do("POST", "/api/suite/run", "", map[string]string{"X-Fixpoint": "1", "Sec-Fetch-Site": "cross-site"}); w.Code != http.StatusForbidden {
		t.Errorf("cross-site: %d %s", w.Code, w.Body)
	}
	if w := r.do("POST", "/api/suite/run", "", map[string]string{"X-Fixpoint": "1", "Sec-Fetch-Site": "same-origin"}); w.Code != http.StatusConflict {
		t.Errorf("empty suite should conflict: %d %s", w.Code, w.Body)
	}
	// No CORS headers on the console API.
	w = r.do("GET", "/api/reports", "", nil)
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("the console API sent CORS headers")
	}
}

func TestPublicAPICORSAndLimits(t *testing.T) {
	r := newRig(t)
	w := r.do("OPTIONS", "/api/public/reports", "", nil)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("preflight: %d %v", w.Code, w.Header())
	}
	// Text length.
	if w := r.do("POST", "/api/public/reports", `{"text":"hi"}`, nil); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "at least 5") {
		t.Errorf("short text: %d %s", w.Code, w.Body)
	}
	long := strings.Repeat("é", 4001)
	if w := r.do("POST", "/api/public/reports", `{"text":"`+long+`"}`, nil); w.Code != http.StatusBadRequest {
		t.Errorf("long text: %d %s", w.Code, w.Body)
	}
	// Body size.
	big := fmt.Sprintf(`{"text":"%s"}`, strings.Repeat("a", 70<<10))
	if w := r.do("POST", "/api/public/reports", big, nil); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("big body: %d %s", w.Code, w.Body)
	}
	if w := r.do("POST", "/api/public/reports", "{not json", nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad json: %d", w.Code)
	}
	// One good report: breadcrumbs and errors are cut to the limits.
	crumbs := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		crumbs = append(crumbs, fmt.Sprintf(`{"kind":"click","text":"%s","ago_ms":%d}`, strings.Repeat("c", 400), i))
	}
	errs := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		errs = append(errs, `"`+strings.Repeat("e", 400)+`"`)
	}
	body := fmt.Sprintf(`{"text":"The total stays the same after I remove an item","context":{"breadcrumbs":[%s],"console_errors":[%s]}}`,
		strings.Join(crumbs, ","), strings.Join(errs, ","))
	w = r.do("POST", "/api/public/reports", body, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var created core.NewReportResponse
	json.Unmarshal(w.Body.Bytes(), &created)
	if created.ID == "" || created.Seq != 1 || !strings.HasSuffix(created.StatusURL, "/r/"+created.ID) || created.EventsURL == "" {
		t.Fatalf("response %+v", created)
	}
	rep, err := r.st.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Context.Breadcrumbs) != MaxBreadcrumbs || len(rep.Context.ConsoleErrors) != MaxConsoleErrors {
		t.Errorf("not cut: %d crumbs, %d errors", len(rep.Context.Breadcrumbs), len(rep.Context.ConsoleErrors))
	}
	if n := len([]rune(rep.Context.Breadcrumbs[0].Text)); n != MaxFieldRunes {
		t.Errorf("breadcrumb text %d runes", n)
	}
	// The newest breadcrumbs are the ones kept.
	if rep.Context.Breadcrumbs[0].AgoMS != 10 {
		t.Errorf("kept the wrong breadcrumbs: first ago_ms %d", rep.Context.Breadcrumbs[0].AgoMS)
	}

	// Rate limit: the burst is already partly spent by the calls above.
	hit429 := false
	for i := 0; i < 10; i++ {
		w := r.do("POST", "/api/public/reports", `{"text":"another report about the total"}`, nil)
		if w.Code == http.StatusTooManyRequests {
			hit429 = true
			if !strings.Contains(w.Body.String(), "wait a few seconds") {
				t.Errorf("429 body %s", w.Body)
			}
			break
		}
	}
	if !hit429 {
		t.Error("the rate limit never fired")
	}
}

func TestRateLimiterRefills(t *testing.T) {
	l := newRateLimiter(2, 10*time.Millisecond)
	if !l.allow("a") || !l.allow("a") || l.allow("a") {
		t.Fatal("burst of 2 not honoured")
	}
	if !l.allow("b") {
		t.Error("a second IP shares the first one's bucket")
	}
	time.Sleep(15 * time.Millisecond)
	if !l.allow("a") {
		t.Error("the bucket did not refill")
	}
}

func TestWaitingLimit(t *testing.T) {
	r := newRig(t)
	for i := 0; i < MaxWaiting; i++ {
		if _, err := r.st.Create(core.Report{Status: core.StatusQueued, Text: "waiting"}); err != nil {
			t.Fatal(err)
		}
	}
	w := r.do("POST", "/api/public/reports", `{"text":"one more report please"}`, nil)
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "as many reports") {
		t.Errorf("waiting limit: %d %s", w.Code, w.Body)
	}
}

func TestReportsAndSlicesAreNeverNull(t *testing.T) {
	r := newRig(t)
	w := r.do("GET", "/api/reports", "", nil)
	if !strings.Contains(w.Body.String(), `"reports":[]`) {
		t.Errorf("empty list: %s", w.Body)
	}
	rep, _ := r.st.Create(core.Report{Status: core.StatusQueued, Text: "a report with no slices set", MaxAttempts: 3})
	w = r.do("GET", "/api/reports/"+rep.ID, "", nil)
	body := w.Body.String()
	for _, want := range []string{`"runs":[]`, `"attempts":[]`, `"thread":[]`, `"breadcrumbs":[]`, `"console_errors":[]`, `"events":[]`, `"track":[`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s in %s", want, body)
		}
	}
	if strings.Contains(body, "null") {
		t.Errorf("a null slipped into the response: %s", body)
	}
	if w := r.do("GET", "/api/reports/r_nope", "", nil); w.Code != http.StatusNotFound {
		t.Errorf("unknown id: %d", w.Code)
	}
}

func TestActionsAndPatch(t *testing.T) {
	r := newRig(t)
	rep, _ := r.st.Create(core.Report{Status: core.StatusError, Text: "x", MaxAttempts: 3,
		Problem: &core.Problem{Stage: "reproduce", Message: "m", Retryable: true}})
	if w := r.do("POST", "/api/reports/"+rep.ID+"/retry", "", fx); w.Code != http.StatusAccepted {
		t.Errorf("retry: %d %s", w.Code, w.Body)
	}
	if w := r.do("POST", "/api/reports/"+rep.ID+"/approve", "", fx); w.Code != http.StatusConflict {
		t.Errorf("approve a queued report: %d %s", w.Code, w.Body)
	}
	if w := r.do("POST", "/api/reports/r_nope/retry", "", fx); w.Code != http.StatusNotFound {
		t.Errorf("retry unknown: %d", w.Code)
	}
	if w := r.do("GET", "/api/reports/"+rep.ID+"/patch", "", nil); w.Code != http.StatusNotFound {
		t.Errorf("patch before any: %d", w.Code)
	}
	dir := filepath.Join(r.root, "artifacts", rep.ID)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "fix.patch"), []byte("--- a/x\n+++ b/x\n"), 0o644)
	w := r.do("GET", "/api/reports/"+rep.ID+"/patch", "", nil)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/x-diff") ||
		!strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("patch: %d %v", w.Code, w.Header())
	}
}

func TestDemoReset(t *testing.T) {
	r := newRig(t)
	if w := r.do("POST", "/api/demo/reset", `{}`, fx); w.Code != http.StatusBadRequest {
		t.Errorf("no confirm: %d %s", w.Code, w.Body)
	}
	if w := r.do("POST", "/api/demo/reset", `{"confirm":"reset"}`, fx); w.Code != http.StatusOK || r.reset != 1 {
		t.Errorf("reset: %d %s (called %d)", w.Code, w.Body, r.reset)
	}
}

func TestArtifactsStayInsideTheirDirectory(t *testing.T) {
	r := newRig(t)
	os.MkdirAll(filepath.Join(r.root, "artifacts", "r_1", "run_1"), 0o755)
	os.WriteFile(filepath.Join(r.root, "artifacts", "r_1", "run_1", "step-1.png"), []byte("PNG"), 0o644)
	os.WriteFile(filepath.Join(r.root, "secret.txt"), []byte("secret"), 0o644)
	if w := r.do("GET", "/artifacts/r_1/run_1/step-1.png", "", nil); w.Code != http.StatusOK || w.Body.String() != "PNG" {
		t.Errorf("artifact: %d %s", w.Code, w.Body)
	}
	for _, p := range []string{"/artifacts/../secret.txt", "/artifacts/r_1/../../secret.txt", "/artifacts/%2e%2e/secret.txt"} {
		w := r.do("GET", p, "", nil)
		if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "secret") {
			t.Errorf("%s escaped the artifacts directory", p)
		}
	}
	if w := r.do("GET", "/artifacts/r_1/run_1/missing.png", "", nil); w.Code != http.StatusNotFound {
		t.Errorf("missing artifact: %d", w.Code)
	}
}

func TestStaticAndRoutes(t *testing.T) {
	r := newRig(t)
	for path, want := range map[string]string{
		"/":              "Fixpoint console",
		"/reports/r_abc": "Fixpoint console",
		"/suite":         "Fixpoint console",
		"/setup":         "Fixpoint console",
		"/app.js":        "export const a = 1;",
		"/r/r_abc":       "Fixpoint status",
		"/widget.js":     "// widget",
	} {
		w := r.do("GET", path, "", nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%s: %d %q", path, w.Code, w.Body.String())
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("%s cache-control %q", path, cc)
		}
	}
	if w := r.do("GET", "/styles.css", "", nil); w.Code != http.StatusNotFound {
		t.Errorf("missing file should be 404, got %d", w.Code)
	}
	if w := r.do("GET", "/api/nope", "", nil); w.Code != http.StatusNotFound {
		t.Errorf("unknown api: %d", w.Code)
	}
	if w := r.do("GET", "/widget.js", "", nil); w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("widget.js needs CORS so other apps can load it")
	}
}

func TestEventStreams(t *testing.T) {
	r := newRig(t)
	srv := httptest.NewServer(r.s)
	defer srv.Close()
	// Console stream, with replay by Last-Event-ID.
	rep, _ := r.st.Create(core.Report{Status: core.StatusQueued, Text: "stream me", MaxAttempts: 3})
	first := r.s.d.Bus.Publish(core.Event{ReportID: rep.ID, Type: core.EvReportCreated, Text: "one"})
	req, _ := http.NewRequest("GET", srv.URL+"/api/events", nil)
	req.Header.Set("Last-Event-ID", fmt.Sprint(first.Seq-1))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	readMessage := func() (id, event, data string) {
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "id: "):
				id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case line == "":
				if event != "" {
					return id, event, data
				}
			}
		}
		return "", "", ""
	}
	id, event, data := readMessage()
	if event != core.EvReportCreated || id != fmt.Sprint(first.Seq) || !strings.Contains(data, `"text":"one"`) {
		t.Errorf("replayed message: id %q event %q data %q", id, event, data)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		r.s.d.Bus.Publish(core.Event{ReportID: rep.ID, Type: core.EvRunStep, Text: "step one", Data: map[string]any{"n": 1}})
	}()
	_, event, data = readMessage()
	if event != core.EvRunStep || !strings.Contains(data, "step one") {
		t.Errorf("live message: %q %q", event, data)
	}

	// Public stream: one update on connect, another on a change.
	resp2, err := http.Get(srv.URL + "/api/public/reports/" + rep.ID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	sc = bufio.NewScanner(resp2.Body)
	_, event, data = readMessage()
	if event != "update" || !strings.Contains(data, `"headline":"Report received"`) {
		t.Errorf("first public message: %q %q", event, data)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		r.st.Update(rep.ID, func(x *core.Report) error { x.Status = core.StatusTriaging; return nil })
		r.s.d.Bus.Publish(core.Event{ReportID: rep.ID, Type: core.EvReportStatus, Text: "triaging"})
	}()
	_, event, data = readMessage()
	if event != "update" || !strings.Contains(data, "Reading your report") {
		t.Errorf("second public message: %q %q", event, data)
	}
	if strings.Contains(data, "app.js") {
		t.Error("the public view leaked file names")
	}
}

func TestPublicReplyFlow(t *testing.T) {
	r := newRig(t)
	rep, _ := r.st.Create(core.Report{Status: core.StatusNeedsInfo, Text: "x", MaxAttempts: 3,
		Thread: []core.Message{{From: "fixpoint", Text: "Which item?"}}})
	if w := r.do("POST", "/api/public/reports/"+rep.ID+"/reply", `{"text":""}`, nil); w.Code != http.StatusBadRequest {
		t.Errorf("empty reply: %d %s", w.Code, w.Body)
	}
	w := r.do("POST", "/api/public/reports/"+rep.ID+"/reply", `{"text":"Masala Chai"}`, nil)
	if w.Code != http.StatusAccepted {
		t.Fatalf("reply: %d %s", w.Code, w.Body)
	}
	got, _ := r.st.Get(rep.ID)
	if len(got.Thread) != 2 || got.Thread[1].From != "reporter" {
		t.Errorf("thread %+v", got.Thread)
	}
	w = r.do("GET", "/api/public/reports/"+rep.ID, "", nil)
	var pr core.PublicReport
	json.Unmarshal(w.Body.Bytes(), &pr)
	if pr.ID != rep.ID || len(pr.Thread) != 2 {
		t.Errorf("public report %+v", pr)
	}
	if w := r.do("GET", "/api/public/reports/r_nope", "", nil); w.Code != http.StatusNotFound {
		t.Errorf("unknown: %d", w.Code)
	}
	if w := r.do("DELETE", "/api/public/reports/"+rep.ID, "", nil); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE: %d", w.Code)
	}
}
