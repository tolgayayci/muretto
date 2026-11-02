package view

import (
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/server/internal/store"
)

// Dashboard review decisions (Keep / Not needed / undo) are merged into the
// synced review items before anything else, one owner at a time: a user's
// decision only ever touches that user's own synced copy, and a decision for
// a resource the user has no synced review item for has no effect.
//
// Latest wins, comparing the CLI copy's decided_at (capped at when the server
// received it, as everywhere in this package) with the decision's server
// time:
//
//   - the CLI copy has no decided_at, or an older one: the dashboard decision
//     applies. "keep" / "not_needed" set state, decided_at (decision time),
//     decided_by (the user's server id) and note. "pending" is an undo: the
//     item shows pending with no decided_at, decided_by or note.
//   - the CLI copy's decided_at is the same or newer: the CLI copy stands.
//
// The team merge (reviewKey) then runs on the adjusted copies, so a member's
// dashboard "keep" never hides another member's pending review.

// applyDecisions adjusts review copies in place. now bounds the decision
// times used for ranking.
func applyDecisions(in map[string][]owned[contract.ReviewItem], decisions []store.ReviewDecision, now time.Time) {
	if len(decisions) == 0 {
		return
	}
	byOwner := make(map[[2]string]store.ReviewDecision, len(decisions))
	for _, d := range decisions {
		byOwner[[2]string{d.UserID, d.ResourceID}] = d
	}
	for _, copies := range in {
		for i := range copies {
			x := &copies[i]
			d, ok := byOwner[[2]string{x.owner, x.v.ResourceID}]
			if !ok {
				continue
			}
			if x.v.DecidedAt != nil && !capped(*x.v.DecidedAt, x.recv).Before(d.DecidedAt) {
				continue // the CLI decided later (or at the same time)
			}
			x.v = decided(x.v, d)
			// The decision time is server time, so it may rank the copy.
			if at := capped(d.DecidedAt, now); at.After(x.recv) {
				x.recv = at
			}
		}
	}
}

// decided returns item with dashboard decision d applied.
func decided(item contract.ReviewItem, d store.ReviewDecision) contract.ReviewItem {
	item.DecidedAt, item.DecidedBy, item.Note = nil, nil, nil
	switch contract.ReviewItemState(d.State) {
	case contract.ReviewItemStateKeep, contract.ReviewItemStateNotNeeded:
		item.State = contract.ReviewItemState(d.State)
		at := d.DecidedAt.UTC().Truncate(time.Second)
		by := d.UserID
		item.DecidedAt, item.DecidedBy = &at, &by
		if d.Note != "" {
			note := d.Note
			item.Note = &note
		}
	case contract.ReviewItemStatePending:
		fallthrough
	default: // pending: undo
		item.State = contract.ReviewItemStatePending
	}
	return item
}
