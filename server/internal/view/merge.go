package view

import (
	"sort"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
)

// The team view merges members' copies of one entity conservatively
// (docs/decisions/0505-team-view-merge.md): merging never makes the view look
// safer than any member's latest report. Stores keep one copy per (member,
// entity), so each copy is that member's latest report.

// owned is one member's stored copy of an entity. recv is when the server
// accepted that copy (store.Record.UpdatedAt, never later than the compose
// time). Client timestamps are capped at recv for ordering only; the shown
// values are the client's originals.
type owned[T any] struct {
	v     T
	owner string
	recv  time.Time
}

// capped is t, but never later than when the server received it.
func capped(t, recv time.Time) time.Time {
	if t.After(recv) {
		return recv
	}
	return t
}

// rankKey orders copies: higher rank wins, then the later time.
type rankKey struct {
	rank int
	at   time.Time
}

// pick returns the copy with the highest key; ties go to the lowest owner id
// so the result is deterministic.
func pick[T any](in []owned[T], key func(owned[T]) rankKey) owned[T] {
	best, bk := in[0], key(in[0])
	for _, x := range in[1:] {
		k := key(x)
		if k.rank > bk.rank || (k.rank == bk.rank && (k.at.After(bk.at) || (k.at.Equal(bk.at) && x.owner < best.owner))) {
			best, bk = x, k
		}
	}
	return best
}

func mergeAll[T any](m map[string][]owned[T], key func(owned[T]) rankKey) map[string]owned[T] {
	out := make(map[string]owned[T], len(m))
	for k, in := range m {
		out[k] = pick(in, key)
	}
	return out
}

func values[T any](m map[string]owned[T]) map[string]T {
	out := make(map[string]T, len(m))
	for k, x := range m {
		out[k] = x.v
	}
	return out
}

func sessionKey(x owned[contract.Session]) rankKey {
	return rankKey{0, capped(activity(x.v), x.recv)}
}

// findingKey: exposed beats unknown beats not_exposed, whoever reported it. A
// member's own later check replaces their earlier copy in the store, so the
// only way an exposure clears is the member who reported it syncing a newer
// check, or leaving the org.
func findingKey(x owned[contract.ExposureFinding]) rankKey {
	r := 0
	switch x.v.State {
	case contract.ExposureFindingStateExposed:
		r = 2
	case contract.ExposureFindingStateUnknown:
		r = 1
	case contract.ExposureFindingStateNotExposed:
	}
	return rankKey{r, capped(timeOf(x.v.CheckedAt), x.recv)}
}

// resourceKey: a resource any member still sees is shown as existing:
// confirmed beats unconfirmed beats gone.
func resourceKey(x owned[contract.Resource]) rankKey {
	r := 0
	switch x.v.Existence {
	case contract.ResourceExistenceConfirmed:
		r = 2
	case contract.ResourceExistenceUnconfirmed:
		r = 1
	case contract.ResourceExistenceGone:
	}
	return rankKey{r, capped(x.v.LastSeenAt, x.recv)}
}

// reviewKey: a pending review beats any decision. Among pending copies the
// earliest due_at wins, so nobody can postpone another member's due review;
// among decisions the newest capped decided_at wins.
func reviewKey(x owned[contract.ReviewItem]) rankKey {
	if x.v.State == contract.ReviewItemStatePending {
		// Mirror due_at around the epoch so "earlier due" is "later key".
		return rankKey{1, time.Unix(0, 0).Add(-x.v.DueAt.Sub(time.Unix(0, 0)))}
	}
	return rankKey{0, capped(timeOf(x.v.DecidedAt), x.recv)}
}

// checkKey: a failed check beats ok or skipped, so a member cannot hide
// another member's failing provider check; otherwise the newest capped.
func checkKey(x owned[contract.ProviderCheck]) rankKey {
	r := 0
	if x.v.Status == contract.ProviderCheckStatusError {
		r = 1
	}
	return rankKey{r, capped(x.v.AttemptedAt, x.recv)}
}

func costKey(x owned[contract.CostRecord]) rankKey {
	return rankKey{0, capped(x.v.AsOf, x.recv)}
}

// checkFreshness is when a check last succeeded, capped at receive time, for
// summary.oldest_check_at.
func checkFreshness(x owned[contract.ProviderCheck]) time.Time {
	t := x.v.AttemptedAt
	if x.v.LastSuccessAt != nil {
		t = *x.v.LastSuccessAt
	}
	return capped(t, x.recv)
}

// mergeAttribution mirrors the CLI's conflict rule (docs/decisions/0401):
// exact claims naming different sessions give unknown with all their
// evidence, so no member can take over another member's exact attribution.
// Exact claims that agree stay exact; with no exact claim the newest capped
// unknown is shown.
func mergeAttribution(in []owned[contract.Attribution]) contract.Attribution {
	var exact []owned[contract.Attribution]
	sessions := map[string]bool{}
	for _, x := range in {
		if x.v.Label == contract.AttributionLabelExact {
			exact = append(exact, x)
			sessions[deref(x.v.SessionID)] = true
		}
	}
	byTime := func(x owned[contract.Attribution]) rankKey { return rankKey{0, capped(x.v.ComputedAt, x.recv)} }
	switch {
	case len(exact) == 0:
		return pick(in, byTime).v
	case len(sessions) == 1:
		return pick(exact, byTime).v
	}
	newest := pick(exact, byTime)
	out := contract.Attribution{
		ResourceID: newest.v.ResourceID,
		Label:      contract.AttributionLabelUnknown,
		Reason:     "team members' syncs attribute this resource to different sessions",
		ComputedAt: capped(newest.v.ComputedAt, newest.recv),
		Evidence:   []contract.Evidence{},
	}
	seen := map[[2]string]bool{}
	for _, x := range exact {
		for _, e := range x.v.Evidence {
			k := [2]string{e.SessionID, e.ActionID}
			if !seen[k] {
				seen[k] = true
				out.Evidence = append(out.Evidence, e)
			}
		}
	}
	sort.Slice(out.Evidence, func(i, j int) bool {
		a, b := out.Evidence[i], out.Evidence[j]
		if !a.OccurredAt.Equal(b.OccurredAt) {
			return a.OccurredAt.Before(b.OccurredAt)
		}
		if a.SessionID != b.SessionID {
			return a.SessionID < b.SessionID
		}
		return a.ActionID < b.ActionID
	})
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
