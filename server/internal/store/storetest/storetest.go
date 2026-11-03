// Package storetest is a conformance suite every store.Store must pass.
package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/server/internal/store"
)

var t0 = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

// Run runs the suite. newStore must return an empty store.
func Run(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Run("Users", func(t *testing.T) { testUsers(t, newStore(t)) })
	t.Run("Tokens", func(t *testing.T) { testTokens(t, newStore(t)) })
	t.Run("Device", func(t *testing.T) { testDevice(t, newStore(t)) })
	t.Run("Orgs", func(t *testing.T) { testOrgs(t, newStore(t)) })
	t.Run("Ingest", func(t *testing.T) { testIngest(t, newStore(t)) })
	t.Run("ReviewDecisions", func(t *testing.T) { testReviewDecisions(t, newStore(t)) })
}

func testReviewDecisions(t *testing.T, s store.Store) {
	ctx := context.Background()
	a, _ := s.UpsertUser(ctx, uniq("supabase:a"), "ana", "", t0)
	b, _ := s.UpsertUser(ctx, uniq("supabase:b"), "ben", "", t0)
	res := "supabase.project:" + uniq("ref")
	set := func(d store.ReviewDecision) {
		t.Helper()
		if err := s.SetReviewDecision(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	set(store.ReviewDecision{UserID: a.ID, ResourceID: res, State: "keep", Note: "prod demo", DecidedAt: t0})
	set(store.ReviewDecision{UserID: b.ID, ResourceID: res, State: "not_needed", DecidedAt: t0})
	// Upsert: a newer decision replaces; an older one is ignored.
	set(store.ReviewDecision{UserID: a.ID, ResourceID: res, State: "pending", DecidedAt: t0.Add(time.Minute)})
	set(store.ReviewDecision{UserID: a.ID, ResourceID: res, State: "not_needed", Note: "stale", DecidedAt: t0.Add(-time.Hour)})
	got, err := s.ReviewDecisions(ctx, []string{a.ID})
	if err != nil || len(got) != 1 {
		t.Fatalf("decisions for a: %+v %v", got, err)
	}
	if d := got[0]; d.UserID != a.ID || d.ResourceID != res || d.State != "pending" || d.Note != "" || !d.DecidedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("decision: %+v", d)
	}
	both, _ := s.ReviewDecisions(ctx, []string{a.ID, b.ID})
	if len(both) != 2 {
		t.Fatalf("decisions for both: %+v", both)
	}
	if none, err := s.ReviewDecisions(ctx, []string{"usr_missing"}); err != nil || none == nil || len(none) != 0 {
		t.Fatalf("decisions for nobody: %#v %v", none, err)
	}
	if err := s.SetReviewDecision(ctx, store.ReviewDecision{UserID: "usr_missing", ResourceID: res, State: "keep", DecidedAt: t0}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("decision for unknown user: %v", err)
	}
}

func uniq(s string) string { return fmt.Sprintf("%s-%d", s, time.Now().UnixNano()) }

func testUsers(t *testing.T, s store.Store) {
	ctx := context.Background()
	id := uniq("github:1")
	u1, err := s.UpsertUser(ctx, id, "alice", "Alice", t0)
	if err != nil {
		t.Fatal(err)
	}
	u2, err := s.UpsertUser(ctx, id, "alice2", "Alice A", t0)
	if err != nil {
		t.Fatal(err)
	}
	if u1.ID != u2.ID || u2.Login != "alice2" {
		t.Fatalf("upsert by identity: %+v %+v", u1, u2)
	}
	got, err := s.GetUser(ctx, u1.ID)
	if err != nil || got.DisplayName != "Alice A" {
		t.Fatalf("GetUser: %+v %v", got, err)
	}
	if _, err := s.GetUser(ctx, "usr_missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing user: %v", err)
	}
}

func testTokens(t *testing.T, s store.Store) {
	ctx := context.Background()
	u, _ := s.UpsertUser(ctx, uniq("github:3"), "carol", "", t0)
	tok := store.Token{ID: store.NewID("tok_"), Hash: uniq("th"), UserID: u.ID, Name: "laptop", CreatedAt: t0}
	if err := s.CreateToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	gt, gu, err := s.TokenUser(ctx, tok.Hash, t0)
	if err != nil || gt.ID != tok.ID || gu.ID != u.ID {
		t.Fatalf("TokenUser: %v", err)
	}
	if !gt.LastUsedAt.Equal(t0) {
		t.Fatalf("first use sets last_used_at: %v", gt.LastUsedAt)
	}
	// Within TokenTouchInterval last_used_at does not move; after it, it does.
	if gt, _, _ := s.TokenUser(ctx, tok.Hash, t0.Add(30*time.Second)); !gt.LastUsedAt.Equal(t0) {
		t.Fatalf("last_used_at moved within the interval: %v", gt.LastUsedAt)
	}
	later := t0.Add(store.TokenTouchInterval + time.Second)
	if gt, _, _ := s.TokenUser(ctx, tok.Hash, later); !gt.LastUsedAt.Equal(later) {
		t.Fatalf("last_used_at after the interval: %v", gt.LastUsedAt)
	}

	// Listing: only the user's own, unrevoked tokens, newest first, with
	// last_used_at.
	tok2 := store.Token{ID: store.NewID("tok_"), Hash: uniq("th2"), UserID: u.ID, Name: "desktop", CreatedAt: t0.Add(time.Hour)}
	other, _ := s.UpsertUser(ctx, uniq("github:3b"), "mallory", "", t0)
	otherTok := store.Token{ID: store.NewID("tok_"), Hash: uniq("th3"), UserID: other.ID, Name: "evil", CreatedAt: t0}
	for _, x := range []store.Token{tok2, otherTok} {
		if err := s.CreateToken(ctx, x); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListTokens(ctx, u.ID)
	if err != nil || len(list) != 2 || list[0].ID != tok2.ID || list[1].ID != tok.ID {
		t.Fatalf("ListTokens: %+v %v", list, err)
	}
	if list[1].Name != "laptop" || !list[1].LastUsedAt.Equal(later) || !list[0].LastUsedAt.IsZero() || !list[1].CreatedAt.Equal(t0) {
		t.Fatalf("listed token fields: %+v", list)
	}
	if none, err := s.ListTokens(ctx, "usr_missing"); err != nil || len(none) != 0 || none == nil {
		t.Fatalf("ListTokens for nobody: %#v %v", none, err)
	}

	// RevokeUserToken: never another user's token.
	if err := s.RevokeUserToken(ctx, u.ID, otherTok.ID, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked another user's token: %v", err)
	}
	if _, _, err := s.TokenUser(ctx, otherTok.Hash, t0); err != nil {
		t.Fatalf("other user's token must survive: %v", err)
	}
	if err := s.RevokeUserToken(ctx, u.ID, "tok_missing", t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown token: %v", err)
	}
	if err := s.RevokeUserToken(ctx, u.ID, tok2.ID, t0); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeUserToken(ctx, u.ID, tok2.ID, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
	if _, _, err := s.TokenUser(ctx, tok2.Hash, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("token revoked by id still valid: %v", err)
	}

	if err := s.RevokeToken(ctx, tok.Hash, t0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.TokenUser(ctx, tok.Hash, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("revoked token still valid: %v", err)
	}
	if err := s.RevokeToken(ctx, tok.Hash, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second RevokeToken: %v", err)
	}
	if list, _ := s.ListTokens(ctx, u.ID); len(list) != 0 {
		t.Fatalf("revoked tokens listed: %+v", list)
	}
}

func testDevice(t *testing.T, s store.Store) {
	ctx := context.Background()
	u, _ := s.UpsertUser(ctx, uniq("github:4"), "dave", "", t0)
	code := fmt.Sprintf("%08d", time.Now().UnixNano()%1e8)
	d := store.DeviceAuth{
		DeviceCodeHash: uniq("dc"), UserCode: code, Client: store.ClientInfo{Host: "laptop"},
		Status: store.DevicePending, Interval: 5 * time.Second, CreatedAt: t0, ExpiresAt: t0.Add(15 * time.Minute),
	}
	if err := s.CreateDeviceAuth(ctx, d); err != nil {
		t.Fatal(err)
	}
	dup := d
	dup.DeviceCodeHash = uniq("dc2")
	if err := s.CreateDeviceAuth(ctx, dup); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("duplicate user code: %v", err)
	}
	got, err := s.DeviceAuthByUserCode(ctx, code, t0)
	if err != nil || got.Client.Host != "laptop" {
		t.Fatalf("by user code: %+v %v", got, err)
	}
	mint := store.Token{ID: store.NewID("tok_"), Hash: uniq("mint"), Name: "laptop", CreatedAt: t0}
	poll := func(at time.Time) store.PollStatus {
		t.Helper()
		r, err := s.PollDeviceAuth(ctx, d.DeviceCodeHash, at, 5*time.Second, mint)
		if err != nil {
			t.Fatal(err)
		}
		return r.Status
	}
	if st := poll(t0); st != store.PollPending {
		t.Fatalf("first poll: %s", st)
	}
	if st := poll(t0.Add(time.Second)); st != store.PollSlowDown {
		t.Fatalf("fast poll: %s", st)
	}
	// Interval is now 10 s.
	if st := poll(t0.Add(8 * time.Second)); st != store.PollSlowDown {
		t.Fatalf("poll inside grown interval: %s", st)
	}
	if err := s.DecideDeviceAuth(ctx, code, u.ID, true, t0.Add(9*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideDeviceAuth(ctx, code, u.ID, true, t0.Add(9*time.Second)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second decision: %v", err)
	}
	r, err := s.PollDeviceAuth(ctx, d.DeviceCodeHash, t0.Add(40*time.Second), 5*time.Second, mint)
	if err != nil || r.Status != store.PollApproved || r.UserID != u.ID {
		t.Fatalf("approved poll: %+v %v", r, err)
	}
	if _, gu, err := s.TokenUser(ctx, mint.Hash, t0); err != nil || gu.ID != u.ID {
		t.Fatalf("minted token: %v", err)
	}
	// A device-flow token without a name is named after the CLI's host.
	mint2 := mint
	mint2.ID, mint2.Hash, mint2.Name = store.NewID("tok_"), uniq("mint2"), ""
	d3 := d
	d3.DeviceCodeHash, d3.UserCode, d3.Client.Host = uniq("dc4"), code[:7]+"Y", "build-box"
	if err := s.CreateDeviceAuth(ctx, d3); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideDeviceAuth(ctx, d3.UserCode, u.ID, true, t0); err != nil {
		t.Fatal(err)
	}
	if r, err := s.PollDeviceAuth(ctx, d3.DeviceCodeHash, t0, time.Second, mint2); err != nil || r.Status != store.PollApproved {
		t.Fatalf("approved poll: %+v %v", r, err)
	}
	if gt, _, err := s.TokenUser(ctx, mint2.Hash, t0); err != nil || gt.Name != "build-box" {
		t.Fatalf("minted token name: %+v %v", gt, err)
	}
	if st := poll(t0.Add(80 * time.Second)); st != store.PollExpired {
		t.Fatalf("consumed code polled again: %s", st)
	}
	if _, err := s.PollDeviceAuth(ctx, "unknown", t0, time.Second, mint); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown device code: %v", err)
	}

	// Denied and expired codes.
	d2 := d
	d2.DeviceCodeHash, d2.UserCode, d2.Interval = uniq("dc3"), code[:7]+"X", time.Second
	if err := s.CreateDeviceAuth(ctx, d2); err != nil {
		t.Fatal(err)
	}
	if err := s.DecideDeviceAuth(ctx, d2.UserCode, u.ID, false, t0); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.PollDeviceAuth(ctx, d2.DeviceCodeHash, t0, time.Second, mint); r.Status != store.PollDenied {
		t.Fatalf("denied: %s", r.Status)
	}
	if r, _ := s.PollDeviceAuth(ctx, d2.DeviceCodeHash, t0.Add(time.Hour), time.Second, mint); r.Status != store.PollExpired {
		t.Fatalf("expired: %s", r.Status)
	}
	if _, err := s.DeviceAuthByUserCode(ctx, d2.UserCode, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("decided code still pending: %v", err)
	}
}

func testOrgs(t *testing.T, s store.Store) {
	ctx := context.Background()
	owner, _ := s.UpsertUser(ctx, uniq("github:5"), "erin", "Erin", t0)
	other, _ := s.UpsertUser(ctx, uniq("github:6"), "frank", "", t0)
	third, _ := s.UpsertUser(ctx, uniq("github:7"), "gina", "", t0)
	org := store.Org{ID: store.NewID("org_"), Name: "Acme", CreatedAt: t0}
	if err := s.CreateOrg(ctx, org, owner.ID, t0); err != nil {
		t.Fatal(err)
	}
	if m, err := s.Membership(ctx, org.ID, owner.ID); err != nil || m.Role != store.RoleOwner {
		t.Fatalf("owner membership: %+v %v", m, err)
	}
	if _, err := s.Membership(ctx, org.ID, other.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("non-member: %v", err)
	}
	inv := store.Invite{Hash: uniq("inv"), OrgID: org.ID, CreatedBy: owner.ID, CreatedAt: t0, ExpiresAt: t0.Add(time.Hour)}
	if err := s.CreateInvite(ctx, inv); err != nil {
		t.Fatal(err)
	}
	// An existing member does not consume the invite.
	if m, err := s.AcceptInvite(ctx, inv.Hash, owner.ID, t0); err != nil || m.Role != store.RoleOwner {
		t.Fatalf("owner accepting: %+v %v", m, err)
	}
	if m, err := s.AcceptInvite(ctx, inv.Hash, other.ID, t0); err != nil || m.Role != store.RoleMember || m.Org.Name != "Acme" {
		t.Fatalf("accept: %+v %v", m, err)
	}
	if _, err := s.AcceptInvite(ctx, inv.Hash, third.ID, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("invite reused: %v", err)
	}
	expired := store.Invite{Hash: uniq("inv2"), OrgID: org.ID, CreatedBy: owner.ID, CreatedAt: t0, ExpiresAt: t0.Add(time.Minute)}
	_ = s.CreateInvite(ctx, expired)
	if _, err := s.AcceptInvite(ctx, expired.Hash, third.ID, t0.Add(time.Hour)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expired invite accepted: %v", err)
	}
	members, err := s.OrgMembers(ctx, org.ID)
	if err != nil || len(members) != 2 {
		t.Fatalf("members: %+v %v", members, err)
	}
	orgs, err := s.UserOrgs(ctx, other.ID)
	if err != nil || len(orgs) != 1 || orgs[0].Org.ID != org.ID {
		t.Fatalf("UserOrgs: %+v %v", orgs, err)
	}
}

func testIngest(t *testing.T, s store.Store) {
	ctx := context.Background()
	u, _ := s.UpsertUser(ctx, uniq("github:8"), "hana", "", t0)
	v, _ := s.UpsertUser(ctx, uniq("github:9"), "ivan", "", t0)
	rec := func(key, rev string) store.Record {
		return store.Record{Kind: store.KindResource, Key: key, Rev: rev, Data: json.RawMessage(`{"id":"` + key + `"}`)}
	}
	in := store.IngestInput{UserID: u.ID, Developer: "hana", BatchID: "b1", Now: t0, Records: []store.Record{rec("r:1", "aaaa"), rec("r:2", "bbbb")}}
	res, err := s.Ingest(ctx, in)
	if err != nil || res.Accepted != 2 || res.Unchanged != 0 || len(res.Events) != 2 {
		t.Fatalf("first ingest: %+v %v", res, err)
	}
	in.Records = []store.Record{rec("r:1", "aaaa"), rec("r:2", "cccc")}
	in.Now = t0.Add(time.Hour)
	res, err = s.Ingest(ctx, in)
	if err != nil || res.Accepted != 1 || res.Unchanged != 1 || len(res.Events) != 1 {
		t.Fatalf("second ingest: %+v %v", res, err)
	}
	if res.Events[0].EntityID != "r:2" || res.Events[0].Developer != "hana" || res.Events[0].Type != "resource" {
		t.Fatalf("event: %+v", res.Events[0])
	}
	lastSeq := res.Events[0].Seq
	_, _ = s.Ingest(ctx, store.IngestInput{UserID: v.ID, Developer: "ivan", BatchID: "b2", Now: t0, Records: []store.Record{rec("r:1", "dddd")}})

	recs, err := s.Records(ctx, []string{u.ID})
	if err != nil || len(recs) != 2 {
		t.Fatalf("records: %d %v", len(recs), err)
	}
	for _, r := range recs {
		if r.UserID != u.ID {
			t.Fatalf("record of another user: %+v", r)
		}
		// UpdatedAt is when the current rev was received; the team view caps
		// client timestamps at it.
		want := t0
		if r.Key == "r:2" {
			want = t0.Add(time.Hour)
		}
		if !r.UpdatedAt.Equal(want) {
			t.Fatalf("record %s updated_at %v, want %v", r.Key, r.UpdatedAt, want)
		}
	}
	both, _ := s.Records(ctx, []string{u.ID, v.ID})
	if len(both) != 3 {
		t.Fatalf("records for both: %d", len(both))
	}
	evs, err := s.EventsAfter(ctx, []string{u.ID}, 0, 100)
	if err != nil || len(evs) != 3 {
		t.Fatalf("events for u: %d %v", len(evs), err)
	}
	evs, _ = s.EventsAfter(ctx, []string{u.ID, v.ID}, lastSeq, 100)
	if len(evs) != 1 || evs[0].Developer != "ivan" {
		t.Fatalf("events after %d: %+v", lastSeq, evs)
	}

	// Large batches produce one bulk event.
	var many []store.Record
	for i := range store.MaxEventsPerBatch + 1 {
		many = append(many, rec(fmt.Sprintf("bulk:%d", i), "eeee"))
	}
	res, _ = s.Ingest(ctx, store.IngestInput{UserID: u.ID, Developer: "hana", BatchID: "b3", Now: t0, Records: many})
	if len(res.Events) != 1 || res.Events[0].Type != store.EventBulk || res.Events[0].EntityID != "b3" {
		t.Fatalf("bulk event: %+v", res.Events)
	}
	if err := s.AddProviderRequest(ctx, store.ProviderRequest{Provider: "neon", At: t0}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddProviderRequest(ctx, store.ProviderRequest{Provider: "Railway", UserID: u.ID, At: t0}); err != nil {
		t.Fatal(err)
	}
}
