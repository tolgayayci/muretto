package answer

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	tf "github.com/tolgayayci/muretto/internal/testfixture"
)

func input() Input {
	return Input{
		Developer: tf.Developer(), DeveloperNames: tf.Names(), Sessions: tf.Sessions(), Actions: tf.Actions(),
		Resources: tf.Resources(), Findings: tf.Findings(), Costs: tf.Costs(), Checks: tf.Checks(),
		Reviews: tf.Reviews(), Hooks: &HookState{Installed: true}, Accounts: 2,
	}
}

func byID[T any](xs []T, id func(T) string) map[string]T {
	m := map[string]T{}
	for _, x := range xs {
		m[id(x)] = x
	}
	return m
}

func TestComputeFixture(t *testing.T) {
	a := Compute(input(), tf.Now)
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := tf.ValidateAnswer(raw); err != nil {
		t.Fatalf("answer does not match schema: %v", err)
	}

	s := a.Summary
	if s.Sessions != 3 || s.AgentCreated != 5 || s.StillExisting != 5 || s.Exposed != 3 || s.NeedsReview != 1 || s.UnknownCreator != 3 {
		t.Fatalf("summary: %+v", s)
	}
	if s.OldestCheckAt == nil || tf.Now.Sub(*s.OldestCheckAt) != 3*time.Minute {
		t.Fatalf("oldest check: %v", s.OldestCheckAt)
	}

	sv := a.Sessions[2]
	if sv.Session.ID != tf.AliceSession || len(sv.ResourceIds) != 5 || sv.ExposedCount != 3 || sv.ReviewDueCount != 1 || sv.DeveloperName != "alice" {
		t.Fatalf("alice view: %+v", sv)
	}

	att := byID(a.Attributions, func(x contract.Attribution) string { return x.ResourceID })
	if l := att["supabase.project:"+tf.ProdRef]; l.Label != contract.AttributionLabelUnknown {
		t.Fatal("prod project was only listed by a session")
	}
	orders := "supabase.table:" + tf.DevRef + "/public.orders"
	res := byID(a.Resources, func(x contract.Resource) string { return x.ID })
	if r, ok := res[orders]; !ok || r.Existence != contract.ResourceExistenceUnconfirmed || r.SeenVia != contract.ResourceSeenViaEvidence || r.AccountID != tf.OrgID {
		t.Fatalf("evidence-only orders table: %+v", r)
	}

	fs := byID(a.Findings, func(x contract.ExposureFinding) string { return x.ID })
	if f := fs["rls_disabled:"+orders]; f.State != contract.ExposureFindingStateNotExposed {
		t.Fatalf("orders: rls check ok and not flagged => not_exposed, got %+v", f)
	}
	// Function has no rule check => no finding; project has none either.
	for _, f := range a.Findings {
		if f.State == contract.ExposureFindingStateNotExposed && f.CheckedAt == nil {
			t.Fatalf("not_exposed without checked_at: %+v", f)
		}
	}

	for _, c := range a.Costs {
		if c.Source == contract.CostRecordSourceUnknown && c.AmountMicros != nil {
			t.Fatalf("unknown cost with an amount: %+v", c)
		}
	}
	costs := byID(a.Costs, func(x contract.CostRecord) string { return x.ResourceID })
	if c := costs["supabase.function:"+tf.DevRef+"/process-import"]; c.Source != contract.CostRecordSourceUnknown {
		t.Fatalf("agent-created resource without cost data must be unknown: %+v", c)
	}

	rv := byID(a.Reviews, func(x contract.ReviewItem) string { return x.ResourceID })
	br := rv["supabase.branch:"+tf.BranchRef]
	if br.State != contract.ReviewItemStatePending || br.OwnerDeveloperID == nil || *br.OwnerDeveloperID != tf.AliceDev ||
		!br.DueAt.Equal(time.Date(2026, 10, 26, 14, 18, 0, 0, time.UTC)) {
		t.Fatalf("branch review: %+v", br)
	}

	codes := map[string]bool{}
	for _, w := range a.Warnings {
		codes[w.Code] = true
	}
	if !codes["transcript_only_sessions"] || codes["stale_data"] || codes["hooks_not_installed"] {
		t.Fatalf("warnings: %+v", a.Warnings)
	}
}

func TestNeverNotExposedByDefault(t *testing.T) {
	in := input()
	in.Checks = nil
	in.Findings = nil
	a := Compute(in, tf.Now)
	for _, f := range a.Findings {
		if f.State == contract.ExposureFindingStateNotExposed {
			t.Fatalf("not_exposed without a check: %+v", f)
		}
	}
	// A failed rule check gives unknown, not not_exposed.
	in = input()
	for i := range in.Checks {
		if in.Checks[i].RuleID != nil {
			in.Checks[i].Status = contract.ProviderCheckStatusError
			msg := "token lacks Advisors (Read)"
			in.Checks[i].Error = &msg
		}
	}
	in.Findings = nil
	a = Compute(in, tf.Now)
	n := 0
	for _, f := range a.Findings {
		if f.State == contract.ExposureFindingStateNotExposed {
			t.Fatalf("not_exposed after a failed check: %+v", f)
		}
		if f.State == contract.ExposureFindingStateUnknown {
			n++
		}
	}
	if n == 0 {
		t.Fatal("want unknown findings")
	}
	warned := false
	for _, w := range a.Warnings {
		warned = warned || w.Code == "check_failed"
	}
	if !warned {
		t.Fatal("want a check_failed warning")
	}
}

func TestStaleAndHooksWarnings(t *testing.T) {
	in := input()
	in.Hooks = &HookState{}
	in.Accounts = 0
	codes := func(now time.Time) map[string]bool {
		m := map[string]bool{}
		for _, w := range Compute(in, now).Warnings {
			m[w.Code] = true
		}
		return m
	}
	// Hooks are not in settings.json, but hook events arrived recently
	// (registered with `claude --settings`, a plugin, ...): no false alarm.
	got := codes(tf.Now.Add(2 * time.Hour))
	for _, c := range []string{"stale_data", "no_providers"} {
		if !got[c] {
			t.Errorf("missing warning %s: %v", c, got)
		}
	}
	if got["hooks_not_installed"] {
		t.Error("hooks_not_installed while hook events are arriving")
	}
	// No hook event for longer than HookWindow: warn.
	if got := codes(tf.Now.Add(HookWindow + 24*time.Hour)); !got["hooks_not_installed"] {
		t.Errorf("missing warning hooks_not_installed: %v", got)
	}
}

func TestHookEventsSeen(t *testing.T) {
	now := tf.Now
	recent := now.Add(-time.Hour)
	old := now.Add(-HookWindow - time.Hour)
	sess := func(src contract.SessionSourcesElem, start time.Time, last *time.Time) contract.Session {
		return contract.Session{ID: "s", StartedAt: start, LastActivityAt: last, Sources: []contract.SessionSourcesElem{src}}
	}
	cases := []struct {
		name string
		in   []contract.Session
		want bool
	}{
		{"none", nil, false},
		{"recent hook", []contract.Session{sess(contract.SessionSourcesElemHook, recent, nil)}, true},
		{"old hook, recent activity", []contract.Session{sess(contract.SessionSourcesElemHook, old, &recent)}, true},
		{"old hook", []contract.Session{sess(contract.SessionSourcesElemHook, old, &old)}, false},
		{"recent transcript only", []contract.Session{sess(contract.SessionSourcesElemTranscript, recent, &recent)}, false},
	}
	for _, c := range cases {
		if got := HookEventsSeen(c.in, now); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestAmbiguousOwnerGivesUnknown(t *testing.T) {
	in := input()
	in.AmbiguousActions = map[string]bool{"toolu_01FAKEaaaaaaaaaaaaaaaaa5": true} // the vercel deploy
	a := Compute(in, tf.Now)
	for _, at := range a.Attributions {
		if at.ResourceID == "vercel.deployment:"+tf.DeployID {
			if at.Label != contract.AttributionLabelUnknown || at.SessionID != nil || len(at.Evidence) == 0 {
				t.Fatalf("want unknown with evidence kept: %+v", at)
			}
			return
		}
	}
	t.Fatal("deployment attribution missing")
}

func TestSubsetRecomputesSummary(t *testing.T) {
	a := Compute(input(), tf.Now)
	sub := Subset(a, func(r contract.Resource) bool { return r.Provider == "vercel" }, func(contract.SessionView) bool { return false })
	if sub.Summary.Exposed != 1 || sub.Summary.AgentCreated != 1 || len(sub.Checks) != 3 || sub.Summary.Sessions != 0 {
		t.Fatalf("subset summary: %+v checks=%d", sub.Summary, len(sub.Checks))
	}
	raw, _ := json.Marshal(sub)
	if err := tf.ValidateAnswer(raw); err != nil {
		t.Fatal(err)
	}
}
