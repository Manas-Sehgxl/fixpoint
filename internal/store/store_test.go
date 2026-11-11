package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
)

func TestCreateUpdateReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Create(core.Report{Status: core.StatusQueued, Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.Create(core.Report{Status: core.StatusQueued, Text: "second"})
	if a.Seq != 1 || b.Seq != 2 || a.ID == b.ID || !strings.HasPrefix(a.ID, "r_") {
		t.Fatalf("ids and sequence: %+v %+v", a, b)
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		t.Error("times not set")
	}
	if _, err := s.Update(a.ID, func(r *core.Report) error {
		r.Status = core.StatusProven
		r.Runs = append(r.Runs, core.Run{ID: "run_1", Status: core.RunPassed})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A failing update changes nothing.
	if _, err := s.Update(a.ID, func(r *core.Report) error { r.Text = "clobbered"; return os.ErrInvalid }); err == nil {
		t.Error("the update error was swallowed")
	}
	if got, _ := s.Get(a.ID); got.Text != "first" || got.Status != core.StatusProven {
		t.Errorf("after a failed update: %+v", got)
	}
	// Reads are copies.
	got, _ := s.Get(a.ID)
	got.Runs[0].Status = core.RunFailed
	if again, _ := s.Get(a.ID); again.Runs[0].Status != core.RunPassed {
		t.Error("Get returned a shared slice")
	}
	if _, err := s.Get("r_nothing"); err != ErrNotFound {
		t.Errorf("unknown id: %v", err)
	}
	s.AddEvent(core.Event{ReportID: a.ID, Type: core.EvReportProven, Text: "proven"}, true)
	test, err := s.AddTest(core.SuiteTest{Title: "T", ReportID: a.ID})
	if err != nil || test.ID != "t_1" {
		t.Fatalf("suite test %+v %v", test, err)
	}
	if err := s.SetTestResult(test.ID, core.SuiteResult{Status: core.RunPassed, Replayed: true}); err != nil {
		t.Fatal(err)
	}

	// Everything survives a reopen, and the counters do not go backwards.
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if list := s2.List(); len(list) != 2 || list[0].Seq != 2 {
		t.Fatalf("list after reopen: %+v", list)
	}
	if evs := s2.AllEvents(); len(evs) != 1 || evs[0].Text != "proven" {
		t.Errorf("events after reopen: %+v", evs)
	}
	if tests := s2.Suite(); len(tests) != 1 || tests[0].LastRun == nil || !tests[0].LastRun.Replayed {
		t.Errorf("suite after reopen: %+v", tests)
	}
	c, _ := s2.Create(core.Report{Text: "third"})
	if c.Seq != 3 {
		t.Errorf("sequence restarted: %d", c.Seq)
	}
	id1, _ := s2.NextRunID()
	id2, _ := s2.NextRunID()
	if id1 == id2 {
		t.Errorf("run ids repeat: %s %s", id1, id2)
	}

	// A damaged file is reported rather than ignored.
	os.WriteFile(filepath.Join(dir, "reports", a.ID+".json"), []byte("{broken"), 0o644)
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Errorf("damaged file: %v", err)
	}
}

func TestResetKeepsEventNumbering(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s, _ := Open(dir)
	r, _ := s.Create(core.Report{Text: "x"})
	e, _ := s.AddEvent(core.Event{ReportID: r.ID, Type: "a"}, true)
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	if len(s.List()) != 0 || len(s.Suite()) != 0 {
		t.Error("reset left data behind")
	}
	e2, _ := s.AddEvent(core.Event{Type: "b"}, false)
	if e2.Seq <= e.Seq {
		t.Errorf("event numbering went backwards: %d then %d", e.Seq, e2.Seq)
	}
	if got, _ := s.Create(core.Report{Text: "y"}); got.Seq != 1 {
		t.Errorf("reports should start again at 1, got %d", got.Seq)
	}
}

func TestConcurrentUpdates(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	r, _ := s.Create(core.Report{Text: "x"})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Update(r.ID, func(rep *core.Report) error {
				rep.Runs = append(rep.Runs, core.Run{ID: "run"})
				return nil
			})
			s.AddEvent(core.Event{ReportID: r.ID, Type: "tick"}, true)
			s.List()
			s.Detail(r.ID)
		}(i)
	}
	wg.Wait()
	got, _ := s.Get(r.ID)
	if len(got.Runs) != 20 {
		t.Errorf("%d runs, want 20", len(got.Runs))
	}
	if evs := s.AllEvents(); len(evs) != 20 {
		t.Errorf("%d events, want 20", len(evs))
	}
	// The file on disk is whole.
	var d core.ReportDetail
	b, err := os.ReadFile(filepath.Join(s.dir, "reports", r.ID+".json"))
	if err != nil || json.Unmarshal(b, &d) != nil || len(d.Events) != 20 {
		t.Errorf("file on disk: %v %d events", err, len(d.Events))
	}
}

func TestWriteFileAtomicLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "f.json")
	if err := WriteFileAtomic(p, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 || entries[0].Name() != "f.json" {
		t.Errorf("directory holds %v", entries)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", info.Mode())
	}
}
