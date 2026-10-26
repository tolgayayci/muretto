package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/tolgayayci/muretto/server/internal/store"
)

// replayLimit caps how many events one reconnect replays.
const replayLimit = 1000

// GET /v1/orgs/{id}/events: Server-Sent Events for the org's members.
//
// Each event is a default ("message") event:
//
//	id: <seq>
//	data: {"seq":42,"type":"resource","id":"supabase.project:abc","developer":"alice","at":"..."}
//
// A comment line ": ping" is sent every heartbeat. On reconnect the browser
// sends Last-Event-ID and missed events are replayed (up to 1000; the
// dashboard should refetch the answer after any reconnect anyway).
func (s *Server) events(w http.ResponseWriter, r *http.Request, p *principal) {
	m, ok := s.member(w, r, p)
	if !ok {
		return
	}
	orgID := m.Org.ID
	memberIDs := func() ([]string, error) {
		ms, err := s.st.OrgMembers(r.Context(), orgID)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(ms))
		for _, mm := range ms {
			ids = append(ids, mm.User.ID)
		}
		return ids, nil
	}
	ids, err := memberIDs()
	if err != nil {
		s.internal(w, r, err)
		return
	}
	var lastID int64
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		lastID, _ = strconv.ParseInt(v, 10, 64)
	}

	// Subscribe before replaying so nothing committed in between is missed;
	// duplicates are dropped by seq.
	sub := s.hub.Subscribe()
	defer s.hub.Unsubscribe(sub)

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{}) // the stream outlives the server's WriteTimeout
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform") // no-transform: proxies must not gzip/buffer the stream
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	sent := map[int64]bool{}
	write := func(e store.Event) error {
		if sent[e.Seq] {
			return nil
		}
		sent[e.Seq] = true
		if len(sent) > 10000 {
			clear(sent)
			sent[e.Seq] = true
		}
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.Seq, data); err != nil {
			return err
		}
		return nil
	}
	if _, err := fmt.Fprint(w, "retry: 3000\n: connected\n\n"); err != nil {
		return
	}
	if lastID > 0 {
		evs, err := s.st.EventsAfter(r.Context(), ids, lastID, replayLimit)
		if err != nil {
			s.log.Warn("sse replay failed", "request_id", requestID(r.Context()), "err", err)
			return
		}
		for _, e := range evs {
			if write(e) != nil {
				return
			}
		}
	}
	if rc.Flush() != nil {
		return
	}

	tick := time.NewTicker(s.opt.Heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		case e, ok := <-sub.C:
			if !ok {
				return // fell behind; the client reconnects with Last-Event-ID
			}
			if !slices.Contains(ids, e.UserID) {
				// Maybe a member who joined after we connected.
				if ids, err = memberIDs(); err != nil || !slices.Contains(ids, e.UserID) {
					continue
				}
			}
			if write(e) != nil || rc.Flush() != nil {
				return
			}
		}
	}
}
