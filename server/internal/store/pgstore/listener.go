package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tolgayayci/muretto/server/internal/store"
)

var errInvalidURL = errors.New("invalid listen URL")

// seenSet remembers recently published sequence numbers. Sequence order is not
// commit order (two concurrent ingests can commit out of order), so events are
// deduplicated by id rather than skipped when lower than the last one seen.
type seenSet struct {
	ids   map[int64]struct{}
	order []int64
	max   int64
}

func newSeenSet() *seenSet { return &seenSet{ids: map[int64]struct{}{}} }

func (s *seenSet) add(seq int64) bool {
	if _, ok := s.ids[seq]; ok {
		return false
	}
	s.ids[seq] = struct{}{}
	s.order = append(s.order, seq)
	if len(s.order) > 10000 {
		for _, old := range s.order[:5000] {
			delete(s.ids, old)
		}
		s.order = append([]int64(nil), s.order[5000:]...)
	}
	s.max = max(s.max, seq)
	return true
}

// Listen holds one dedicated session-level connection, LISTENs on Channel and
// passes every event to publish. After a reconnect it replays recent events
// from the events table, so live updates survive restarts and reach every
// instance. It returns when ctx is done.
//
// url must be a direct or session-mode pooler URL: LISTEN does not work
// through a transaction-mode pooler.
func (s *Store) Listen(ctx context.Context, url string, publish func([]store.Event), log *slog.Logger) {
	seen := newSeenSet()
	if last, err := s.MaxSeq(ctx); err == nil {
		seen.max = last
	} else {
		log.Warn("event listener: reading last sequence failed", "err", err)
	}
	backoff := time.Second
	for ctx.Err() == nil {
		err := s.listenOnce(ctx, url, seen, publish)
		if ctx.Err() != nil {
			return
		}
		log.Warn("event listener disconnected; retrying", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// catchUpWindow re-reads this many sequence numbers below the newest seen one
// after a reconnect, to pick up transactions that committed out of order.
const catchUpWindow = 200

func (s *Store) listenOnce(ctx context.Context, url string, seen *seenSet, publish func([]store.Event)) error {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return errInvalidURL
	}
	cfg.RuntimeParams["application_name"] = "murettod-listener"
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	if _, err := conn.Exec(ctx, "listen "+Channel); err != nil {
		return err
	}
	// Catch up on anything committed while we were not listening.
	after := max(seen.max-catchUpWindow, 0)
	for {
		evs, err := s.EventsAfter(ctx, nil, after, 1000)
		if err != nil {
			return err
		}
		if len(evs) == 0 {
			break
		}
		var fresh []store.Event
		for _, e := range evs {
			if seen.add(e.Seq) {
				fresh = append(fresh, e)
			}
		}
		if len(fresh) > 0 {
			publish(fresh)
		}
		after = evs[len(evs)-1].Seq
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var p notifyPayload
		if json.Unmarshal([]byte(n.Payload), &p) != nil || !seen.add(p.Seq) {
			continue
		}
		publish([]store.Event{{Seq: p.Seq, UserID: p.UserID, Type: p.Type, EntityID: p.EntityID, Developer: p.Developer, At: p.At}})
	}
}
