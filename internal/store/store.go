// Package store keeps reports, their events, the regression suite and
// counters as JSON files under the data directory. Every write goes to a
// temporary file first and is renamed into place, so a crash never leaves a
// half-written file behind.
package store

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
)

// ErrNotFound is returned for an unknown report id.
var ErrNotFound = errors.New("no report with that id")

type meta struct {
	NextSeq   int   `json:"next_seq"`
	NextEvent int64 `json:"next_event"`
	NextTest  int   `json:"next_test"`
	NextRun   int   `json:"next_run"`
}

type suiteFile struct {
	Tests []core.SuiteTest `json:"tests"`
}

// Store is safe for concurrent use. Reads return deep copies.
type Store struct {
	dir     string
	mu      sync.Mutex
	reports map[string]*core.ReportDetail
	suite   []core.SuiteTest
	meta    meta
}

// Open loads (or creates) the store rooted at dir, the ".fixpoint/data"
// directory.
func Open(dir string) (*Store, error) {
	s := &Store{dir: dir, reports: map[string]*core.ReportDetail{}}
	if err := os.MkdirAll(filepath.Join(dir, "reports"), 0o755); err != nil {
		return nil, fmt.Errorf("create the data directory: %w", err)
	}
	if err := readJSON(filepath.Join(dir, "meta.json"), &s.meta); err != nil {
		return nil, err
	}
	var sf suiteFile
	if err := readJSON(filepath.Join(dir, "suite.json"), &sf); err != nil {
		return nil, err
	}
	s.suite = sf.Tests
	entries, err := os.ReadDir(filepath.Join(dir, "reports"))
	if err != nil {
		return nil, fmt.Errorf("read the reports directory: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var d core.ReportDetail
		if err := readJSON(filepath.Join(dir, "reports", e.Name()), &d); err != nil {
			return nil, err
		}
		if d.Report.ID == "" {
			continue
		}
		s.reports[d.Report.ID] = &d
		// Keep counters ahead of what is on disk even if meta.json is stale.
		if d.Report.Seq >= s.meta.NextSeq {
			s.meta.NextSeq = d.Report.Seq
		}
		for _, ev := range d.Events {
			if ev.Seq > s.meta.NextEvent {
				s.meta.NextEvent = ev.Seq
			}
		}
	}
	return s, nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s is damaged (%v); move it aside to continue", path, err)
	}
	return nil
}

// WriteFileAtomic writes data to path through a temporary file and a rename.
func WriteFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, b)
}

func clone[T any](v T) T {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

func (s *Store) saveReport(d *core.ReportDetail) error {
	return writeJSON(filepath.Join(s.dir, "reports", d.Report.ID+".json"), d)
}

func (s *Store) saveMeta() error { return writeJSON(filepath.Join(s.dir, "meta.json"), s.meta) }

func (s *Store) saveSuite() error {
	return writeJSON(filepath.Join(s.dir, "suite.json"), suiteFile{Tests: s.suite})
}

const idAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// NewID returns an unguessable report id such as "r_7f3k2m9qxa".
func NewID() string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = idAlphabet[int(b[i])%len(idAlphabet)]
	}
	return "r_" + string(b)
}

// Create stores a new report, assigning its id, sequence number and times.
func (s *Store) Create(r core.Report) (core.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta.NextSeq++
	r.ID = NewID()
	r.Seq = s.meta.NextSeq
	now := time.Now().UTC()
	r.CreatedAt, r.UpdatedAt = now, now
	d := &core.ReportDetail{Report: r}
	if err := s.saveMeta(); err != nil {
		return core.Report{}, fmt.Errorf("save counters: %w", err)
	}
	if err := s.saveReport(d); err != nil {
		return core.Report{}, fmt.Errorf("save the report: %w", err)
	}
	s.reports[r.ID] = d
	return clone(r), nil
}

// Get returns a copy of one report.
func (s *Store) Get(id string) (core.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.reports[id]
	if !ok {
		return core.Report{}, ErrNotFound
	}
	return clone(d.Report), nil
}

// Detail returns a copy of one report with its events.
func (s *Store) Detail(id string) (core.ReportDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.reports[id]
	if !ok {
		return core.ReportDetail{}, ErrNotFound
	}
	return clone(*d), nil
}

// Update applies fn to the stored report and saves it. If fn returns an
// error nothing changes.
func (s *Store) Update(id string, fn func(*core.Report) error) (core.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.reports[id]
	if !ok {
		return core.Report{}, ErrNotFound
	}
	r := clone(d.Report)
	if err := fn(&r); err != nil {
		return core.Report{}, err
	}
	r.UpdatedAt = time.Now().UTC()
	old := d.Report
	d.Report = r
	if err := s.saveReport(d); err != nil {
		d.Report = old
		return core.Report{}, fmt.Errorf("save the report: %w", err)
	}
	return clone(r), nil
}

// List returns copies of every report, newest first.
func (s *Store) List() []core.Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.Report, 0, len(s.reports))
	for _, d := range s.reports {
		out = append(out, clone(d.Report))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq > out[j].Seq })
	return out
}

// AddEvent assigns the next sequence number and time to e and, when persist
// is true and the event belongs to a report, appends it to that report's
// file.
func (s *Store) AddEvent(e core.Event, persist bool) (core.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta.NextEvent++
	e.Seq = s.meta.NextEvent
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	if e.Data == nil {
		e.Data = map[string]any{}
	}
	if !persist {
		return e, nil
	}
	if err := s.saveMeta(); err != nil {
		return e, fmt.Errorf("save counters: %w", err)
	}
	if e.ReportID == "" {
		return e, nil
	}
	d, ok := s.reports[e.ReportID]
	if !ok {
		return e, nil
	}
	d.Events = append(d.Events, clone(e))
	return e, s.saveReport(d)
}

// AllEvents returns every stored event, oldest first.
func (s *Store) AllEvents() []core.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []core.Event
	for _, d := range s.reports {
		out = append(out, clone(d.Events)...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// NextRunID returns a run id that is unique across the data directory, so
// artifact folders never collide.
func (s *Store) NextRunID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta.NextRun++
	return fmt.Sprintf("run_%d", s.meta.NextRun), s.saveMeta()
}

// Suite returns copies of every suite test, oldest first.
func (s *Store) Suite() []core.SuiteTest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.suite)
}

// AddTest stores a new suite test and returns it with its id.
func (s *Store) AddTest(t core.SuiteTest) (core.SuiteTest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta.NextTest++
	t.ID = fmt.Sprintf("t_%d", s.meta.NextTest)
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	s.suite = append(s.suite, t)
	if err := s.saveMeta(); err != nil {
		return t, err
	}
	return clone(t), s.saveSuite()
}

// SetTestResult records the latest result of one suite test.
func (s *Store) SetTestResult(id string, res core.SuiteResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.suite {
		if s.suite[i].ID == id {
			r := res
			s.suite[i].LastRun = &r
			return s.saveSuite()
		}
	}
	return fmt.Errorf("no suite test %s", id)
}

// Reset deletes every report, the suite and the counters.
func (s *Store) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.RemoveAll(s.dir); err != nil {
		return fmt.Errorf("clear the data directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(s.dir, "reports"), 0o755); err != nil {
		return err
	}
	// The event counter keeps rising so open event streams stay ordered.
	s.meta = meta{NextEvent: s.meta.NextEvent}
	s.reports = map[string]*core.ReportDetail{}
	s.suite = nil
	return s.saveMeta()
}
