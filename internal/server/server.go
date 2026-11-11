// Package server is Fixpoint's HTTP surface: the console API for the local
// console, the public API for the widget and the status page, the event
// streams, the artifacts and the embedded browser files.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
	"github.com/Manas-Sehgxl/fixpoint/internal/events"
	"github.com/Manas-Sehgxl/fixpoint/internal/pipeline"
	"github.com/Manas-Sehgxl/fixpoint/internal/store"
	"github.com/Manas-Sehgxl/fixpoint/internal/view"
)

// Deps is what the server needs from the rest of the engine.
type Deps struct {
	Store        *store.Store
	Bus          *events.Bus
	Worker       *pipeline.Worker
	System       func() core.System
	ArtifactsDir string
	Console      fs.FS
	Widget       fs.FS
	// ResetDemo re-seeds the demo shop and clears the data; nil outside demo mode.
	ResetDemo func() error
	// AllowedHost, when set, is accepted as the console API's Host besides
	// loopback names.
	AllowedHost string
}

// Server routes every request.
type Server struct {
	d     Deps
	mux   *http.ServeMux
	limit *rateLimiter
}

// New builds the handler.
func New(d Deps) *Server {
	s := &Server{d: d, mux: http.NewServeMux(), limit: newRateLimiter(5, 10*time.Second)}
	s.routes()
	return s
}

// ServeHTTP makes Server an http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) routes() {
	// Console API: loopback only, no CORS.
	console := func(h http.HandlerFunc) http.Handler { return s.guardConsole(h) }
	s.mux.Handle("GET /api/system", console(s.getSystem))
	s.mux.Handle("GET /api/reports", console(s.getReports))
	s.mux.Handle("GET /api/reports/{id}", console(s.getReport))
	s.mux.Handle("POST /api/reports/{id}/retry", console(s.postRetry))
	s.mux.Handle("POST /api/reports/{id}/approve", console(s.postApprove))
	s.mux.Handle("POST /api/reports/{id}/reply", console(s.postReply))
	s.mux.Handle("GET /api/reports/{id}/patch", console(s.getPatch))
	s.mux.Handle("GET /api/suite", console(s.getSuite))
	s.mux.Handle("POST /api/suite/run", console(s.postSuiteRun))
	s.mux.Handle("POST /api/demo/reset", console(s.postDemoReset))
	s.mux.Handle("GET /api/events", console(s.d.Bus.ServeAll))
	s.mux.Handle("GET /artifacts/", console(s.getArtifact))

	// Public API: open to the app the widget runs in.
	public := func(h http.HandlerFunc) http.Handler { return cors(h) }
	s.mux.Handle("/api/public/reports", public(s.publicReports))
	s.mux.Handle("/api/public/reports/{id}", public(s.publicReport))
	s.mux.Handle("/api/public/reports/{id}/reply", public(s.publicReply))
	s.mux.Handle("/api/public/reports/{id}/events", public(s.publicEvents))
	s.mux.Handle("GET /widget.js", public(s.getWidget))

	s.mux.HandleFunc("GET /r/{id}", s.statusPage)
	s.mux.HandleFunc("/", s.static)
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, code int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"error":"The response could not be encoded."}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	w.Write(b)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// errStatus maps an action's error to a status code and a sentence.
func errStatus(err error) (int, string) {
	var c *pipeline.Conflict
	switch {
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "No report with that id."
	case errors.As(err, &c):
		return http.StatusConflict, c.Msg
	}
	return http.StatusInternalServerError, err.Error()
}

func (s *Server) report(id string) (core.Report, error) {
	r, err := s.d.Store.Get(id)
	if err != nil {
		return r, err
	}
	s.d.Worker.Fill(&r)
	return r, nil
}

func decodeBody(w http.ResponseWriter, r *http.Request, max int64, v any) bool {
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, max+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "The request body could not be read.")
		return false
	}
	if int64(len(b)) > max {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("The request body is larger than %d KB.", max/1024))
		return false
	}
	if len(b) == 0 {
		b = []byte("{}")
	}
	if err := json.Unmarshal(b, v); err != nil {
		writeError(w, http.StatusBadRequest, "The request body is not valid JSON.")
		return false
	}
	return true
}

// --- console API guards ---

// isLocalHost reports whether the Host header names this machine, which is
// the only way the console API may be reached.
func (s *Server) isLocalHost(hostHeader string) bool {
	if hostHeader == "" {
		return false
	}
	if s.d.AllowedHost != "" && strings.EqualFold(hostHeader, s.d.AllowedHost) {
		return true
	}
	host := hostHeader
	if h, _, err := net.SplitHostPort(hostHeader); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// guardConsole refuses console API requests that do not come from the
// console running on this machine.
func (s *Server) guardConsole(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.isLocalHost(r.Host) {
			writeError(w, http.StatusForbidden, "The Fixpoint console API answers only on this machine.")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-Fixpoint") != "1" {
				writeError(w, http.StatusForbidden, "This request needs the header X-Fixpoint: 1.")
				return
			}
			if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
				writeError(w, http.StatusForbidden, "Cross-site requests are refused.")
				return
			}
		}
		h(w, r)
	})
}

// cors answers preflights and allows any origin, because the widget runs in
// the app being watched.
func cors(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h2 := w.Header()
		h2.Set("Access-Control-Allow-Origin", "*")
		h2.Set("Access-Control-Allow-Headers", "Content-Type")
		h2.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h2.Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h(w, r)
	})
}

// --- console API ---

func (s *Server) getSystem(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.d.System())
}

func (s *Server) getReports(w http.ResponseWriter, r *http.Request) {
	reports := s.d.Store.List()
	list := core.ReportList{Reports: []core.ReportSummary{}}
	for i := range reports {
		s.d.Worker.Fill(&reports[i])
		list.Reports = append(list.Reports, view.Summary(reports[i]))
	}
	list.Stats = view.Stats(reports, len(s.d.Store.Suite()))
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) getReport(w http.ResponseWriter, r *http.Request) {
	d, err := s.d.Store.Detail(r.PathValue("id"))
	if err != nil {
		code, msg := errStatus(err)
		writeError(w, code, msg)
		return
	}
	s.d.Worker.Fill(&d.Report)
	d.Events = view.NormalizeEvents(d.Events)
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) postRetry(w http.ResponseWriter, r *http.Request) {
	s.action(w, s.d.Worker.Retry(r.PathValue("id")), "The report is queued again.")
}

func (s *Server) postApprove(w http.ResponseWriter, r *http.Request) {
	s.action(w, s.d.Worker.Approve(r.PathValue("id")), "The fix is queued.")
}

func (s *Server) postReply(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if !decodeBody(w, r, 8<<10, &body) {
		return
	}
	s.action(w, s.d.Worker.Reply(r.PathValue("id"), body.Text), "The reply was added.")
}

func (s *Server) action(w http.ResponseWriter, err error, ok string) {
	if err != nil {
		code, msg := errStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": ok})
}

func (s *Server) getPatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rep, err := s.d.Store.Get(id)
	if err != nil {
		code, msg := errStatus(err)
		writeError(w, code, msg)
		return
	}
	patch := ""
	if b, err := os.ReadFile(filepath.Join(s.d.ArtifactsDir, id, "fix.patch")); err == nil {
		patch = string(b)
	}
	if patch == "" {
		for i := len(rep.Attempts) - 1; i >= 0 && patch == ""; i-- {
			for _, f := range rep.Attempts[i].Files {
				patch += f.Patch
			}
		}
	}
	if patch == "" {
		writeError(w, http.StatusNotFound, "This report has no patch yet.")
		return
	}
	w.Header().Set("Content-Type", "text/x-diff; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"fixpoint-%d.patch\"", rep.Seq))
	w.Header().Set("Cache-Control", "no-store")
	io.WriteString(w, patch)
}

func (s *Server) getSuite(w http.ResponseWriter, r *http.Request) {
	tests := s.d.Store.Suite()
	if tests == nil {
		tests = []core.SuiteTest{}
	}
	writeJSON(w, http.StatusOK, core.Suite{Tests: tests, Running: s.d.Worker.SuiteRunning()})
}

func (s *Server) postSuiteRun(w http.ResponseWriter, r *http.Request) {
	s.action(w, s.d.Worker.RunSuite(), "The suite is queued.")
}

func (s *Server) postDemoReset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Confirm string `json:"confirm"`
	}
	if !decodeBody(w, r, 1<<10, &body) {
		return
	}
	if body.Confirm != "reset" {
		writeError(w, http.StatusBadRequest, `Send {"confirm":"reset"} to clear the demo.`)
		return
	}
	if s.d.ResetDemo == nil {
		writeError(w, http.StatusConflict, "Only the built-in demo can be reset.")
		return
	}
	if err := s.d.ResetDemo(); err != nil {
		writeError(w, http.StatusInternalServerError, "The demo could not be reset: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "The demo shop, the reports and the suite were reset."})
}

func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/artifacts/")
	clean := path.Clean("/" + rel)
	full := filepath.Join(s.d.ArtifactsDir, filepath.FromSlash(clean))
	base, err := filepath.Abs(s.d.ArtifactsDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The artifacts directory could not be read.")
		return
	}
	abs, err := filepath.Abs(full)
	if err != nil || (abs != base && !strings.HasPrefix(abs, base+string(filepath.Separator))) {
		writeError(w, http.StatusForbidden, "That path is outside the artifacts directory.")
		return
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "No such artifact.")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, abs)
}
