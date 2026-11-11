package server

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
	"github.com/Manas-Sehgxl/fixpoint/internal/events"
	"github.com/Manas-Sehgxl/fixpoint/internal/view"
)

// Limits on the public report endpoint. Each report spends Kane credits and
// agent tokens, so they are strict.
const (
	MaxBodyBytes     = 64 << 10
	MinTextRunes     = 5
	MaxTextRunes     = 4000
	MaxBreadcrumbs   = 30
	MaxConsoleErrors = 20
	MaxFieldRunes    = 300
	MaxWaiting       = 25
	RateBurst        = 5
	RateEvery        = 10 * time.Second
)

// rateLimiter is a per-IP token bucket: RateBurst at once, one more every
// RateEvery.
type rateLimiter struct {
	burst int
	every time.Duration
	mu    sync.Mutex
	seen  map[string]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newRateLimiter(burst int, every time.Duration) *rateLimiter {
	return &rateLimiter{burst: burst, every: every, seen: map[string]*bucket{}}
}

// allow takes one token for key, reporting whether there was one.
func (l *rateLimiter) allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.seen[key]
	if b == nil {
		b = &bucket{tokens: float64(l.burst), at: now}
		l.seen[key] = b
	}
	b.tokens += now.Sub(b.at).Seconds() / l.every.Seconds()
	if b.tokens > float64(l.burst) {
		b.tokens = float64(l.burst)
	}
	b.at = now
	if len(l.seen) > 4096 {
		for k, v := range l.seen {
			if now.Sub(v.at) > time.Hour {
				delete(l.seen, k)
			}
		}
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func clipRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// sanitize cuts a new report down to the limits and reports the first
// problem with it, as a sentence for the reporter.
func sanitize(req *core.NewReportRequest) (string, int) {
	req.Text = strings.TrimSpace(req.Text)
	n := utf8.RuneCountInString(req.Text)
	switch {
	case n < MinTextRunes:
		return fmt.Sprintf("Please say a little more: at least %d characters.", MinTextRunes), http.StatusBadRequest
	case n > MaxTextRunes:
		return fmt.Sprintf("That is longer than %d characters; please shorten it.", MaxTextRunes), http.StatusBadRequest
	}
	req.Reporter.Name = clipRunes(req.Reporter.Name, 120)
	req.Reporter.Email = clipRunes(req.Reporter.Email, 200)
	c := &req.Context
	c.PageURL = clipRunes(c.PageURL, 500)
	c.UserAgent = clipRunes(c.UserAgent, MaxFieldRunes)
	if len(c.Breadcrumbs) > MaxBreadcrumbs {
		c.Breadcrumbs = c.Breadcrumbs[len(c.Breadcrumbs)-MaxBreadcrumbs:]
	}
	for i := range c.Breadcrumbs {
		c.Breadcrumbs[i].Text = clipRunes(c.Breadcrumbs[i].Text, MaxFieldRunes)
		c.Breadcrumbs[i].Kind = clipRunes(c.Breadcrumbs[i].Kind, 20)
	}
	if len(c.ConsoleErrors) > MaxConsoleErrors {
		c.ConsoleErrors = c.ConsoleErrors[len(c.ConsoleErrors)-MaxConsoleErrors:]
	}
	for i := range c.ConsoleErrors {
		c.ConsoleErrors[i] = clipRunes(c.ConsoleErrors[i], MaxFieldRunes)
	}
	if c.Breadcrumbs == nil {
		c.Breadcrumbs = []core.Breadcrumb{}
	}
	if c.ConsoleErrors == nil {
		c.ConsoleErrors = []string{}
	}
	return "", 0
}

// waiting counts the reports that are queued or waiting on a person, which
// is what the "too many waiting" limit protects.
func (s *Server) waiting() int {
	n := 0
	for _, r := range s.d.Store.List() {
		if r.Status.Active() || r.Status == core.StatusNeedsInfo || r.Status == core.StatusAwaitingApproval {
			n++
		}
	}
	return n
}

func (s *Server) publicReports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Send a POST to file a report.")
		return
	}
	if !s.limit.allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "That is a lot of reports at once. Please wait a few seconds and try again.")
		return
	}
	if s.waiting() >= MaxWaiting {
		writeError(w, http.StatusTooManyRequests, "Fixpoint already has as many reports as it can work on. Please try again later.")
		return
	}
	var req core.NewReportRequest
	if !decodeBody(w, r, MaxBodyBytes, &req) {
		return
	}
	if msg, code := sanitize(&req); msg != "" {
		writeError(w, code, msg)
		return
	}
	rep, err := s.d.Worker.Submit(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "The report could not be saved: "+err.Error())
		return
	}
	origin := s.d.System().ConsoleURL
	writeJSON(w, http.StatusCreated, core.NewReportResponse{
		ID: rep.ID, Seq: rep.Seq,
		StatusURL: origin + "/r/" + rep.ID,
		EventsURL: origin + "/api/public/reports/" + rep.ID + "/events",
	})
}

func (s *Server) publicReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Only GET is allowed here.")
		return
	}
	rep, err := s.report(r.PathValue("id"))
	if err != nil {
		code, msg := errStatus(err)
		writeError(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, view.PublicReport(rep))
}

func (s *Server) publicReply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Send a POST to reply.")
		return
	}
	if !s.limit.allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "Too many replies at once. Please wait a few seconds and try again.")
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if !decodeBody(w, r, MaxBodyBytes, &body) {
		return
	}
	body.Text = clipRunes(body.Text, MaxTextRunes)
	if utf8.RuneCountInString(body.Text) < 1 {
		writeError(w, http.StatusBadRequest, "The reply is empty.")
		return
	}
	s.action(w, s.d.Worker.Reply(r.PathValue("id"), body.Text), "Thank you: your answer was added.")
}

// publicEvents streams the reporter's view of one report: once on connect,
// then on every change to that report.
func (s *Server) publicEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Only GET is allowed here.")
		return
	}
	id := r.PathValue("id")
	rep, err := s.report(id)
	if err != nil {
		code, msg := errStatus(err)
		writeError(w, code, msg)
		return
	}
	ch, _, cancel := s.d.Bus.Subscribe(-1)
	defer cancel()
	send, fl, err := events.SSE(w)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if send("", "update", view.PublicReport(rep)) != nil {
		return
	}
	tick := time.NewTicker(events.Heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			if e.ReportID != id {
				continue
			}
			rep, err := s.report(id)
			if err != nil {
				return
			}
			if send("", "update", view.PublicReport(rep)) != nil {
				return
			}
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}
