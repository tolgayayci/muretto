package view

import (
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/server/internal/fixture"
	"github.com/tolgayayci/muretto/server/internal/store"
	"github.com/tolgayayci/muretto/server/internal/validate"
)

const (
	alice = "usr_a"
	bob   = "usr_b"
	resID = "supabase.project:projaaaaaaaaaaaaaaaa"
)

func rec(user string, kind store.Kind, key string, recv time.Time, v any) store.Record {
	return store.Record{UserID: user, Kind: kind, Key: key, Rev: "r", Data: fixture.JSON(v), UpdatedAt: recv}
}

func baseFinding() contract.ExposureFinding {
	return fixture.Batch("alice", "projaaaaaaaaaaaaaaaa").Findings[0].Entity
}

func finding(state contract.ExposureFindingState, checked time.Time) contract.ExposureFinding {
	f := baseFinding()
	f.State = state
	f.CheckedAt = &checked
	if state != contract.ExposureFindingStateExposed {
		f.Severity = contract.ExposureFindingSeverityInfo
	}
	return f
}

func compose(t *testing.T, recs ...store.Record) contract.Answer {
	t.Helper()
	a, err := Compose(recs, map[string]string{alice: "Alice", bob: "Bob"}, Viewer{UserID: alice}, fixture.T0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	v, err := validate.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Answer(fixture.JSON(a)); err != nil {
		t.Fatalf("answer invalid: %v", err)
	}
	return a
}

func oneFinding(t *testing.T, a contract.Answer) contract.ExposureFinding {
	t.Helper()
	if len(a.Findings) != 1 {
		t.Fatalf("findings: %d", len(a.Findings))
	}
	return a.Findings[0]
}

// The reviewer's scenario: bob's not_exposed for alice's exposed finding,
// stamped five years ahead, must not hide the exposure.
func TestOtherMemberCannotClearExposure(t *testing.T) {
	t0 := fixture.T0
	f := baseFinding()
	for name, bobChecked := range map[string]time.Time{"later": t0.Add(time.Minute), "future": t0.AddDate(5, 0, 0)} {
		t.Run(name, func(t *testing.T) {
			a := compose(t,
				rec(alice, store.KindFinding, f.ID, t0, finding(contract.ExposureFindingStateExposed, t0)),
				rec(bob, store.KindFinding, f.ID, t0.Add(time.Minute), finding(contract.ExposureFindingStateNotExposed, bobChecked)),
			)
			if got := oneFinding(t, a); got.State != contract.ExposureFindingStateExposed || a.Summary.Exposed != 1 {
				t.Fatalf("state %s exposed %d", got.State, a.Summary.Exposed)
			}
		})
	}
	// unknown also outranks not_exposed.
	a := compose(t,
		rec(alice, store.KindFinding, f.ID, t0, finding(contract.ExposureFindingStateUnknown, t0)),
		rec(bob, store.KindFinding, f.ID, t0, finding(contract.ExposureFindingStateNotExposed, t0.AddDate(5, 0, 0))),
	)
	if got := oneFinding(t, a); got.State != contract.ExposureFindingStateUnknown {
		t.Fatalf("state %s", got.State)
	}
}

// A future checked_at is capped at receive time: among equally alarming
// reports it cannot outrank one received later, and its original value is
// kept in the answer.
func TestFutureTimestampCappedAtReceiveTime(t *testing.T) {
	t0 := fixture.T0
	future := t0.AddDate(5, 0, 0)
	f := baseFinding()
	skewed := finding(contract.ExposureFindingStateExposed, future)
	skewed.Title = "skewed"
	honest := finding(contract.ExposureFindingStateExposed, t0.Add(10*time.Minute))
	honest.Title = "honest"
	a := compose(t,
		rec(bob, store.KindFinding, f.ID, t0, skewed),
		rec(alice, store.KindFinding, f.ID, t0.Add(10*time.Minute), honest),
	)
	if got := oneFinding(t, a); got.Title != "honest" {
		t.Fatalf("future-stamped copy outranked a later report: %s", got.Title)
	}
	a = compose(t, rec(bob, store.KindFinding, f.ID, t0, skewed))
	if got := oneFinding(t, a); !got.CheckedAt.Equal(future) {
		t.Fatalf("original checked_at not kept: %v", got.CheckedAt)
	}

	// Resources: a future-stamped "gone" cannot hide one another member sees.
	res := fixture.Batch("alice", "projaaaaaaaaaaaaaaaa").Resources[0].Entity
	gone := res
	gone.Existence = contract.ResourceExistenceGone
	gone.LastSeenAt = future
	gone.GoneAt = &future
	a = compose(t,
		rec(alice, store.KindResource, res.ID, t0, res),
		rec(bob, store.KindResource, res.ID, t0, gone),
	)
	if a.Resources[0].Existence != contract.ResourceExistenceConfirmed {
		t.Fatalf("resource hidden as %s", a.Resources[0].Existence)
	}

	// Reviews: a decision or a postponed due date cannot close a due review.
	rv := fixture.Batch("alice", "projaaaaaaaaaaaaaaaa").Reviews[0].Entity
	decided := rv
	decided.State = contract.ReviewItemStateNotNeeded
	decided.DecidedAt = &future
	postponed := rv
	postponed.DueAt = future
	for _, other := range []contract.ReviewItem{decided, postponed} {
		a = compose(t,
			rec(alice, store.KindReview, resID, t0, rv),
			rec(bob, store.KindReview, resID, t0, other),
		)
		if a.Summary.NeedsReview != 1 {
			t.Fatalf("due review hidden by %+v", other)
		}
	}

	// Checks: a future-stamped ok cannot hide a failing check, nor make the
	// oldest successful check look fresher than its receive time.
	ck := fixture.Batch("alice", "projaaaaaaaaaaaaaaaa").Checks[0].Entity
	failed := ck
	failed.Status = contract.ProviderCheckStatusError
	failed.LastSuccessAt = nil
	okFuture := ck
	okFuture.AttemptedAt, okFuture.LastSuccessAt = future, &future
	key := store.CheckKey(ck)
	a = compose(t,
		rec(alice, store.KindCheck, key, t0, failed),
		rec(bob, store.KindCheck, key, t0, okFuture),
	)
	if a.Summary.ChecksFailed != 1 || a.Summary.ChecksOk != 0 {
		t.Fatalf("failing check hidden: %+v", a.Summary)
	}
	a = compose(t, rec(bob, store.KindCheck, key, t0, okFuture))
	if a.Summary.OldestCheckAt == nil || !a.Summary.OldestCheckAt.Equal(t0) {
		t.Fatalf("oldest_check_at not capped: %v", a.Summary.OldestCheckAt)
	}
}

// A member's own later not_exposed (after fixing the table) clears their own
// earlier exposure: the store keeps one copy per member, so the newer check
// replaces the old one.
func TestSameOwnerUpdateClearsExposure(t *testing.T) {
	t0 := fixture.T0
	f := baseFinding()
	a := compose(t, rec(alice, store.KindFinding, f.ID, t0, finding(contract.ExposureFindingStateExposed, t0)))
	if a.Summary.Exposed != 1 {
		t.Fatalf("exposed %d", a.Summary.Exposed)
	}
	a = compose(t, rec(alice, store.KindFinding, f.ID, t0.Add(time.Minute), finding(contract.ExposureFindingStateNotExposed, t0.Add(time.Minute))))
	if got := oneFinding(t, a); got.State != contract.ExposureFindingStateNotExposed || a.Summary.Exposed != 0 {
		t.Fatalf("state %s exposed %d", got.State, a.Summary.Exposed)
	}
	// Once every reporting member has re-checked, the team view is clear too.
	a = compose(t,
		rec(alice, store.KindFinding, f.ID, t0.Add(time.Minute), finding(contract.ExposureFindingStateNotExposed, t0.Add(time.Minute))),
		rec(bob, store.KindFinding, f.ID, t0.Add(2*time.Minute), finding(contract.ExposureFindingStateNotExposed, t0.Add(2*time.Minute))),
	)
	if a.Summary.Exposed != 0 {
		t.Fatalf("exposed %d", a.Summary.Exposed)
	}
}

// Two members claiming different sessions for one resource give unknown with
// both evidences; a newer or future-stamped claim cannot take over.
func TestConflictingAttributions(t *testing.T) {
	t0 := fixture.T0
	mine := fixture.Batch("alice", "projaaaaaaaaaaaaaaaa").Attributions[0].Entity
	theirs := fixture.Batch("bob", "projaaaaaaaaaaaaaaaa").Attributions[0].Entity
	theirs.ComputedAt = t0.AddDate(5, 0, 0)
	if *mine.SessionID == *theirs.SessionID {
		t.Fatal("fixture sessions must differ")
	}
	a := compose(t,
		rec(alice, store.KindAttribution, resID, t0, mine),
		rec(bob, store.KindAttribution, resID, t0.Add(time.Minute), theirs),
	)
	if len(a.Attributions) != 1 {
		t.Fatalf("attributions %d", len(a.Attributions))
	}
	at := a.Attributions[0]
	if at.Label != contract.AttributionLabelUnknown || at.SessionID != nil || len(at.Evidence) != 2 {
		t.Fatalf("conflict not unknown with both evidences: %+v", at)
	}
	if at.ComputedAt.After(t0.Add(time.Minute)) {
		t.Fatalf("computed_at not capped: %v", at.ComputedAt)
	}
	if a.Summary.AgentCreated != 0 || a.Summary.UnknownCreator != 0 {
		// No resource record in this answer, so unknown_creator stays 0.
		t.Fatalf("summary %+v", a.Summary)
	}

	// Agreeing claims stay exact; an unknown never overrides an exact claim.
	unknown := mine
	unknown.Label, unknown.SessionID, unknown.ComputedAt = contract.AttributionLabelUnknown, nil, t0.AddDate(5, 0, 0)
	a = compose(t,
		rec(alice, store.KindAttribution, resID, t0, mine),
		rec(bob, store.KindAttribution, resID, t0, mine),
		rec("usr_c", store.KindAttribution, resID, t0, unknown),
	)
	if at := a.Attributions[0]; at.Label != contract.AttributionLabelExact || *at.SessionID != *mine.SessionID {
		t.Fatalf("agreeing claims: %+v", at)
	}
}
