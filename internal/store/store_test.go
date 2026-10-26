package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
)

func ptr[T any](v T) *T { return &v }

var t0 = time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)

func open(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "muretto.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("db mode %v", st.Mode().Perm())
	}
	return s
}

func TestWALAndReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "muretto.db")
	ctx := context.Background()
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := s.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode %q %v", mode, err)
	}
	if err := s.SetMeta(ctx, "k", "v"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, ok, _ := s.Meta(ctx, "k"); !ok || v != "v" {
		t.Fatal("meta lost on reopen")
	}
}

func session(id string, start time.Time, src contract.SessionSourcesElem) contract.Session {
	return contract.Session{ID: id, DeveloperID: "dev", Agent: contract.SessionAgentClaudeCode, StartedAt: start,
		Sources: []contract.SessionSourcesElem{src}, TitleSource: contract.SessionTitleSourceNone}
}

func TestSessionMergeAndActionDedupe(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	hook := session("s1", t0.Add(time.Minute), contract.SessionSourcesElemHook)
	tr := session("s1", t0, contract.SessionSourcesElemTranscript)
	tr.TaskTitle, tr.TitleSource = ptr("ai title"), contract.SessionTitleSourceAiTitle
	tr.GitBranch = ptr("main")
	hook.TaskTitle, hook.TitleSource = ptr("add waitlist"), contract.SessionTitleSourceFirstPrompt
	fork := session("s2", t0.Add(time.Hour), contract.SessionSourcesElemTranscript)

	act := func(sess string, src contract.AgentActionSource) Action {
		return Action{Action: contract.AgentAction{ID: "toolu_1", SessionID: sess, OccurredAt: t0, ToolName: "Bash",
			Status: contract.AgentActionStatusOk, Source: src}}
	}
	if err := s.SaveBatch(ctx, []contract.Session{hook, fork}, []Action{act("s2", contract.AgentActionSourceTranscript)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveBatch(ctx, []contract.Session{tr}, []Action{act("s1", contract.AgentActionSourceTranscript)}); err != nil {
		t.Fatal(err)
	}
	ss, _ := s.Sessions(ctx)
	var got contract.Session
	for _, x := range ss {
		if x.ID == "s1" {
			got = x
		}
	}
	if !got.StartedAt.Equal(t0) || *got.TaskTitle != "add waitlist" || got.GitBranch == nil || len(got.Sources) != 2 {
		t.Fatalf("merge: %+v", got)
	}
	acts, _ := s.Actions(ctx)
	if len(acts) != 1 || acts[0].SessionID != "s1" {
		t.Fatalf("dedupe: earliest session should win, got %+v", acts)
	}
	raw, _ := s.RawActions(ctx, true)
	if len(raw) != 1 {
		t.Fatal("pending extraction")
	}
	if err := s.SetIdentifiers(ctx, map[string][]contract.Identifier{"toolu_1": {{Kind: "vercel.deployment", Provider: "vercel", Value: "dpl_FAKE", Role: contract.IdentifierRoleCreated, SignatureID: "x"}}}); err != nil {
		t.Fatal(err)
	}
	raw, _ = s.RawActions(ctx, true)
	acts, _ = s.Actions(ctx)
	if len(raw) != 0 || len(acts[0].Identifiers) != 1 || *acts[0].ProviderGuess != "vercel" {
		t.Fatalf("identifiers: %+v", acts)
	}
}

func res(id string) contract.Resource {
	kind := id[:len("supabase.bucket")]
	return contract.Resource{ID: id, Kind: kind, Provider: "supabase", ProviderID: id[len(kind)+1:], Name: "b",
		ParentID: ptr("supabase.project:projaaaaaaaaaaaaaaaa"), Attributes: contract.ResourceAttributes{},
		Existence: contract.ResourceExistenceConfirmed, SeenVia: contract.ResourceSeenViaList}
}

func listCheck(status contract.ProviderCheckStatus, at time.Time) contract.ProviderCheck {
	return contract.ProviderCheck{Provider: "supabase", AccountID: "org", Kind: "supabase.bucket",
		Scope: ptr("projaaaaaaaaaaaaaaaa"), Status: status, AttemptedAt: at}
}

func TestGoneOnlyAfterSuccessfulScanAndHistoryKept(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	b := res("supabase.bucket:projaaaaaaaaaaaaaaaa/b")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.ApplyScan(ctx, "supabase", "org", Scan{Resources: []contract.Resource{b}, Checks: []contract.ProviderCheck{listCheck("ok", t0)}}, t0))
	// Failed check: missing resource is not gone.
	must(s.ApplyScan(ctx, "supabase", "org", Scan{Checks: []contract.ProviderCheck{listCheck("error", t0.Add(time.Hour))}}, t0.Add(time.Hour)))
	rs, _ := s.Resources(ctx)
	if rs[0].Existence != contract.ResourceExistenceConfirmed {
		t.Fatalf("failed scan marked gone: %+v", rs[0])
	}
	cs, _ := s.Checks(ctx)
	if cs[0].Status != "error" || cs[0].LastSuccessAt == nil || !cs[0].LastSuccessAt.Equal(t0) {
		t.Fatalf("last_success_at not carried: %+v", cs[0])
	}
	// Successful check without it: gone, history kept.
	must(s.ApplyScan(ctx, "supabase", "org", Scan{Checks: []contract.ProviderCheck{listCheck("ok", t0.Add(2*time.Hour))}}, t0.Add(2*time.Hour)))
	rs, _ = s.Resources(ctx)
	if len(rs) != 1 || rs[0].Existence != contract.ResourceExistenceGone || rs[0].GoneAt == nil {
		t.Fatalf("want gone: %+v", rs)
	}
	// Comes back: confirmed, gone_at kept (recreated), first_seen kept.
	must(s.ApplyScan(ctx, "supabase", "org", Scan{Resources: []contract.Resource{b}, Checks: []contract.ProviderCheck{listCheck("ok", t0.Add(3*time.Hour))}}, t0.Add(3*time.Hour)))
	rs, _ = s.Resources(ctx)
	if rs[0].Existence != contract.ResourceExistenceConfirmed || rs[0].GoneAt == nil || !rs[0].FirstSeenAt.Equal(t0) {
		t.Fatalf("returned: %+v", rs[0])
	}
	ev, _ := s.ResourceEvents(ctx, b.ID)
	if len(ev) != 3 || ev[0].Event != "seen" || ev[1].Event != "gone" || ev[2].Event != "returned" {
		t.Fatalf("events: %+v", ev)
	}
	if v, _ := s.MetaTime(ctx, ScanKey("supabase", "org")); !v.Equal(t0.Add(3 * time.Hour)) {
		t.Fatalf("scan time %v", v)
	}
}

func TestFindingsReplacedOnlyBySuccessfulRuleCheck(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	b := res("supabase.bucket:projaaaaaaaaaaaaaaaa/b")
	rule := func(st contract.ProviderCheckStatus, at time.Time) contract.ProviderCheck {
		c := listCheck(st, at)
		c.RuleID = ptr("public_bucket")
		return c
	}
	f := contract.ExposureFinding{ID: "public_bucket:" + b.ID, ResourceID: b.ID, RuleID: "public_bucket", State: "exposed",
		Severity: "high", Signal: "api_field", Title: "public", Explanation: "x", Fix: contract.Fix{Kind: "steps", Text: "make it private"}}
	unknownCost := contract.CostRecord{ResourceID: b.ID, Source: "unknown", AmountMicros: ptr(0), Basis: "none", AsOf: t0}
	if err := s.ApplyScan(ctx, "supabase", "org", Scan{Resources: []contract.Resource{b}, Findings: []contract.ExposureFinding{f},
		Costs: []contract.CostRecord{unknownCost}, Checks: []contract.ProviderCheck{rule("ok", t0)}}, t0); err != nil {
		t.Fatal(err)
	}
	cs, _ := s.Costs(ctx)
	if len(cs) != 1 || cs[0].AmountMicros != nil {
		t.Fatalf("unknown cost must have no amount: %+v", cs)
	}
	_ = s.ApplyScan(ctx, "supabase", "org", Scan{Resources: []contract.Resource{b}, Checks: []contract.ProviderCheck{rule("error", t0.Add(time.Hour))}}, t0.Add(time.Hour))
	if fs, _ := s.Findings(ctx); len(fs) != 1 {
		t.Fatal("failed check dropped a finding")
	}
	_ = s.ApplyScan(ctx, "supabase", "org", Scan{Resources: []contract.Resource{b}, Checks: []contract.ProviderCheck{rule("ok", t0.Add(2*time.Hour))}}, t0.Add(2*time.Hour))
	if fs, _ := s.Findings(ctx); len(fs) != 0 {
		t.Fatal("successful check should clear the fixed finding")
	}
}

func TestReviewsAndSyncRevs(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	if err := s.SetReview(ctx, contract.ReviewItem{ResourceID: "x:y", State: "keep", DueAt: t0}); err != nil {
		t.Fatal(err)
	}
	if rs, _ := s.Reviews(ctx); len(rs) != 1 || rs[0].State != "keep" {
		t.Fatal("review")
	}
	if err := s.SetSyncRevs(ctx, map[string]string{"resource:x": "abcd"}); err != nil {
		t.Fatal(err)
	}
	if m, _ := s.SyncRevs(ctx); m["resource:x"] != "abcd" {
		t.Fatal("revs")
	}
}
