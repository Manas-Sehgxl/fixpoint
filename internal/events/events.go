// Package events publishes timeline events: it numbers and persists each one
// through the store, keeps recent ones in memory for stream replay, fans them
// out to subscribers and serves them as server-sent events.
package events

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Manas-Sehgxl/fixpoint/internal/core"
)

// Store is the persistence the bus needs.
type Store interface {
	AddEvent(e core.Event, persist bool) (core.Event, error)
	AllEvents() []core.Event
}

const ringSize = 4000

// Bus is safe for concurrent use.
type Bus struct {
	store Store
	mu    sync.Mutex
	ring  []core.Event
	subs  map[chan core.Event]struct{}
}

// New returns a bus primed with the stored events, for replay.
func New(s Store) *Bus {
	b := &Bus{store: s, subs: map[chan core.Event]struct{}{}}
	all := s.AllEvents()
	if len(all) > ringSize {
		all = all[len(all)-ringSize:]
	}
	b.ring = all
	return b
}

// Publish numbers, stores and broadcasts an event. Live step events
// (run.step) are broadcast and kept for replay but not written to the report
// file: the steps themselves are saved on the run.
func (b *Bus) Publish(e core.Event) core.Event {
	e, err := b.store.AddEvent(e, e.Type != core.EvRunStep)
	if err != nil {
		log.Printf("fixpoint: could not save event %s: %v", e.Type, err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ring = append(b.ring, e)
	if len(b.ring) > ringSize {
		b.ring = append([]core.Event(nil), b.ring[len(b.ring)-ringSize:]...)
	}
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
			// A subscriber that cannot keep up is dropped; its client
			// reconnects with Last-Event-ID and replays what it missed.
			delete(b.subs, ch)
			close(ch)
		}
	}
	return e
}

// Subscribe returns a channel of new events and the events after since that
// are still in memory. Call the returned function to unsubscribe.
func (b *Bus) Subscribe(since int64) (<-chan core.Event, []core.Event, func()) {
	ch := make(chan core.Event, 256)
	b.mu.Lock()
	var backlog []core.Event
	if since >= 0 {
		for _, e := range b.ring {
			if e.Seq > since {
				backlog = append(backlog, e)
			}
		}
	}
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, backlog, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
	}
}

// Heartbeat is how often an idle stream sends a comment line.
var Heartbeat = 15 * time.Second

// SSE prepares w for a server-sent event stream and returns a function that
// writes one message.
func SSE(w http.ResponseWriter) (func(id, event string, data any) error, http.Flusher, error) {
	fl, ok := w.(http.Flusher)
	if !ok {
		return nil, nil, fmt.Errorf("streaming is not supported by this connection")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	send := func(id, event string, data any) error {
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if id != "" {
			if _, err := fmt.Fprintf(w, "id: %s\n", id); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return err
		}
		fl.Flush()
		return nil
	}
	return send, fl, nil
}

// ServeAll streams every event. It honours Last-Event-ID and ?since=<seq> by
// first replaying newer events from memory.
func (b *Bus) ServeAll(w http.ResponseWriter, r *http.Request) {
	since := int64(-1)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			since = n
		}
	}
	if v := r.URL.Query().Get("since"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			since = n
		}
	}
	ch, backlog, cancel := b.Subscribe(since)
	defer cancel()
	send, fl, err := SSE(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	last := int64(0)
	emit := func(e core.Event) error {
		if e.Seq <= last {
			return nil
		}
		last = e.Seq
		return send(strconv.FormatInt(e.Seq, 10), e.Type, e)
	}
	for _, e := range backlog {
		if emit(e) != nil {
			return
		}
	}
	tick := time.NewTicker(Heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok || emit(e) != nil {
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
