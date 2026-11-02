package view

import (
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/server/internal/fixture"
	"github.com/tolgayayci/muretto/server/internal/store"
	"github.com/tolgayayci/muretto/server/internal/validate"
)

// review is a CLI-synced review item for resID, due before T0.
func review(state contract.ReviewItemState, decidedAt *time.Time) contract.ReviewItem {
	r := contract.ReviewItem{ResourceID: resID, DueAt: fixture.T0.Add(-time.Hour), State: state}
	if decidedAt != nil {
		by := "dev-local"
		note := "from cli"
		r.DecidedAt, r.DecidedBy, r.Note = decidedAt, &by, &note
	}
	return r
}

func at(d time.Duration) *time.Time { t := fixture.T0.Add(d); return &t }

func composeD(t *testing.T, viewer string, decisions []store.ReviewDecision, recs ...store.Record) contract.Answer {
	t.Helper()
	a, err := ComposeWithDecisions(recs, decisions, map[string]string{alice: "Alice", bob: "Bob"}, Viewer{UserID: viewer}, fixture.T0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	v, err := validate.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Answer(fixture.JSON(a)); err != nil {
		t.Fatalf("answer does not match the schema: %v", err)
	}
	return a
}

func dec(user, state string, d time.Duration) store.ReviewDecision {
	return store.ReviewDecision{UserID: user, ResourceID: resID, State: state, Note: "from dashboard", DecidedAt: fixture.T0.Add(d)}
}

func TestDecisionsLatestWins(t *testing.T) {
	recv := fixture.T0.Add(30 * time.Minute) // when the CLI copy reached the server
	cases := []struct {
		name      string
		cli       contract.ReviewItem
		decisions []store.ReviewDecision
		state     contract.ReviewItemState
		byUser    bool // decided_by is the user's server id, note from the dashboard
		needs     int
	}{
		{"no decision: CLI pending stands", review(contract.ReviewItemStatePending, nil), nil, contract.ReviewItemStatePending, false, 1},
		{"dashboard keep over CLI pending", review(contract.ReviewItemStatePending, nil), []store.ReviewDecision{dec(alice, "keep", 0)}, contract.ReviewItemStateKeep, true, 0},
		{"dashboard not_needed over older CLI keep", review(contract.ReviewItemStateKeep, at(-time.Hour)), []store.ReviewDecision{dec(alice, "not_needed", 0)}, contract.ReviewItemStateNotNeeded, true, 0},
		{"newer CLI keep over older dashboard not_needed", review(contract.ReviewItemStateKeep, at(10*time.Minute)), []store.ReviewDecision{dec(alice, "not_needed", 0)}, contract.ReviewItemStateKeep, false, 0},
		{"same time: CLI stands", review(contract.ReviewItemStateKeep, at(0)), []store.ReviewDecision{dec(alice, "not_needed", 0)}, contract.ReviewItemStateKeep, false, 0},
		{"CLI future decided_at is capped at receive time", review(contract.ReviewItemStateKeep, at(48*time.Hour)), []store.ReviewDecision{dec(alice, "not_needed", 40*time.Minute)}, contract.ReviewItemStateNotNeeded, true, 0},
		{"undo over older CLI keep", review(contract.ReviewItemStateKeep, at(-time.Hour)), []store.ReviewDecision{dec(alice, "pending", 0)}, contract.ReviewItemStatePending, false, 1},
		{"newer CLI keep over older undo", review(contract.ReviewItemStateKeep, at(10*time.Minute)), []store.ReviewDecision{dec(alice, "pending", 0)}, contract.ReviewItemStateKeep, false, 0},
		{"decision for another resource: no effect", review(contract.ReviewItemStatePending, nil),
			[]store.ReviewDecision{{UserID: alice, ResourceID: "supabase.project:other", State: "keep", DecidedAt: fixture.T0}}, contract.ReviewItemStatePending, false, 1},
		{"another user's decision: no effect", review(contract.ReviewItemStatePending, nil), []store.ReviewDecision{dec(bob, "keep", 0)}, contract.ReviewItemStatePending, false, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := composeD(t, alice, c.decisions, rec(alice, store.KindReview, resID, recv, c.cli))
			if len(a.Reviews) != 1 {
				t.Fatalf("reviews: %+v", a.Reviews)
			}
			r := a.Reviews[0]
			if r.State != c.state || a.Summary.NeedsReview != c.needs {
				t.Fatalf("state %s needs_review %d, want %s %d", r.State, a.Summary.NeedsReview, c.state, c.needs)
			}
			switch {
			case c.byUser:
				if r.DecidedBy == nil || *r.DecidedBy != alice || r.DecidedAt == nil || r.Note == nil || *r.Note != "from dashboard" {
					t.Fatalf("dashboard decision fields: %+v", r)
				}
			case r.State == contract.ReviewItemStatePending && len(c.decisions) > 0 && c.decisions[0].UserID == alice && c.decisions[0].ResourceID == resID:
				if r.DecidedBy != nil || r.DecidedAt != nil || r.Note != nil {
					t.Fatalf("undo must clear decided_by/decided_at/note: %+v", r)
				}
			default:
				if c.cli.DecidedBy != nil && (r.DecidedBy == nil || *r.DecidedBy != "dev-local") {
					t.Fatalf("CLI copy changed: %+v", r)
				}
			}
		})
	}
}

// Team view: a member's dashboard "keep" applies to their own copy only and
// never hides another member's pending review.
func TestDecisionsTeamIsolation(t *testing.T) {
	recv := fixture.T0.Add(30 * time.Minute)
	recs := []store.Record{
		rec(alice, store.KindReview, resID, recv, review(contract.ReviewItemStatePending, nil)),
		rec(bob, store.KindReview, resID, recv, review(contract.ReviewItemStatePending, nil)),
	}
	a := composeD(t, alice, []store.ReviewDecision{dec(alice, "keep", 0)}, recs...)
	if a.Reviews[0].State != contract.ReviewItemStatePending || a.Summary.NeedsReview != 1 {
		t.Fatalf("alice's keep hid bob's pending review: %+v", a.Reviews)
	}
	both := composeD(t, alice, []store.ReviewDecision{dec(alice, "keep", 0), dec(bob, "not_needed", time.Minute)}, recs...)
	if r := both.Reviews[0]; r.State != contract.ReviewItemStateNotNeeded || *r.DecidedBy != bob || both.Summary.NeedsReview != 0 {
		t.Fatalf("both decided: newest decision wins: %+v", r)
	}
	// Bob's own view never sees alice's decision.
	b := composeD(t, bob, []store.ReviewDecision{dec(alice, "keep", 0)}, recs[1])
	if b.Reviews[0].State != contract.ReviewItemStatePending {
		t.Fatalf("alice's decision leaked into bob's view: %+v", b.Reviews)
	}
}
