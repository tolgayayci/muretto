// Package events fans live-update events out to SSE subscribers in this
// process. In production it is fed by Postgres LISTEN/NOTIFY (pgstore.Listen);
// with the memory store the API publishes to it directly after each ingest.
package events

import (
	"sync"

	"github.com/tolgayayci/muretto/server/internal/store"
)

// Sub is one subscriber. C is closed when the subscriber falls behind (its
// buffer filled up) or unsubscribes; an SSE client then reconnects and
// replays with Last-Event-ID.
type Sub struct {
	C    chan store.Event
	done bool
}

// Hub is an in-process broadcaster.
type Hub struct {
	mu   sync.Mutex
	subs map[*Sub]struct{}
}

// NewHub returns an empty hub.
func NewHub() *Hub { return &Hub{subs: map[*Sub]struct{}{}} }

// SubBuffer is each subscriber's channel capacity.
const SubBuffer = 256

// Subscribe registers a subscriber.
func (h *Hub) Subscribe() *Sub {
	s := &Sub{C: make(chan store.Event, SubBuffer)}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

// Unsubscribe removes s and closes its channel.
func (h *Hub) Unsubscribe(s *Sub) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.drop(s)
}

func (h *Hub) drop(s *Sub) {
	if !s.done {
		s.done = true
		delete(h.subs, s)
		close(s.C)
	}
}

// Publish sends events to every subscriber without blocking.
func (h *Hub) Publish(evs []store.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		for _, e := range evs {
			select {
			case s.C <- e:
			default:
				h.drop(s) // slow consumer: it will reconnect and replay
			}
			if s.done {
				break
			}
		}
	}
}

// Subscribers returns the current subscriber count (tests, metrics).
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
