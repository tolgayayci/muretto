package view

import (
	"encoding/json"
	"testing"

	"github.com/tolgayayci/muretto/server/internal/fixture"
	"github.com/tolgayayci/muretto/server/internal/store"
	"github.com/tolgayayci/muretto/server/internal/validate"
)

func records(userID string, b any) []store.Record {
	var raw struct {
		Sessions, Resources, Attributions, Findings, Costs, Reviews, Checks []struct {
			Rev    string          `json:"rev"`
			Entity json.RawMessage `json:"entity"`
		}
	}
	_ = json.Unmarshal(fixture.JSON(b), &raw)
	var out []store.Record
	add := func(k store.Kind, items []struct {
		Rev    string          `json:"rev"`
		Entity json.RawMessage `json:"entity"`
	}) {
		for i, it := range items {
			out = append(out, store.Record{UserID: userID, Kind: k, Key: string(k) + string(rune('0'+i)), Rev: it.Rev, Data: it.Entity})
		}
	}
	add(store.KindSession, raw.Sessions)
	add(store.KindResource, raw.Resources)
	add(store.KindAttribution, raw.Attributions)
	add(store.KindFinding, raw.Findings)
	add(store.KindCost, raw.Costs)
	add(store.KindReview, raw.Reviews)
	add(store.KindCheck, raw.Checks)
	return out
}

func TestComposeTeamUnion(t *testing.T) {
	recs := append(records("usr_a", fixture.Batch("alice", "projaaaaaaaaaaaaaaaa")), records("usr_b", fixture.Batch("bob", "projbbbbbbbbbbbbbbbb"))...)
	// bob also synced alice's project (same deterministic id): no duplicate.
	recs = append(recs, records("usr_b", fixture.Batch("bob", "projaaaaaaaaaaaaaaaa"))[1])
	names := map[string]string{"usr_a": "Alice", "usr_b": "Bob"}
	a, err := Compose(recs, names, Viewer{UserID: "usr_a", Login: "alice"}, fixture.T0)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Sessions) != 2 || len(a.Resources) != 2 {
		t.Fatalf("sessions %d resources %d", len(a.Sessions), len(a.Resources))
	}
	got := map[string]bool{}
	for _, s := range a.Sessions {
		got[s.DeveloperName] = true
		if len(s.ResourceIds) != 1 || s.ExposedCount != 1 || s.ReviewDueCount != 1 {
			t.Fatalf("session view: %+v", s)
		}
	}
	if !got["Alice"] || !got["Bob"] {
		t.Fatalf("developer names: %v", got)
	}
	s := a.Summary
	if s.Sessions != 2 || s.AgentCreated != 2 || s.StillExisting != 2 || s.Exposed != 2 || s.NeedsReview != 2 || s.UnknownCreator != 0 || s.ChecksOk != 1 {
		t.Fatalf("summary: %+v", s)
	}
	v, err := validate.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Answer(fixture.JSON(a)); err != nil {
		t.Fatalf("composed answer does not match answer.schema.json: %v", err)
	}
	empty, _ := Compose(nil, nil, Viewer{UserID: "usr_c"}, fixture.T0)
	if err := v.Answer(fixture.JSON(empty)); err != nil {
		t.Fatalf("empty answer invalid: %v", err)
	}
}
