package syncclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/server/testserver"
)

var t0 = time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

// fakeToken builds a clearly fake Muretto token at runtime.
func fakeToken() string { return "mrt_" + strings.Repeat("abcdefgh", 4) }

func sampleAnswer() contract.Answer {
	sid := "00000000-0000-4000-8000-000000000001"
	rid := "supabase.project:abcdefghijklmnopqrst"
	creds := "https://deploy:" + "hunter2" + "@github.com/example/app.git?ref=x#frag"
	return contract.Answer{
		SchemaVersion: 1, GeneratedAt: t0,
		Developer: contract.Developer{ID: "dev-1", DisplayName: "Alice", MachineIDHash: "0123456789abcdef"},
		Sessions: []contract.SessionView{{DeveloperName: "Alice", ResourceIds: []string{rid}, Session: contract.Session{
			ID: sid, Agent: contract.SessionAgentClaudeCode, DeveloperID: "dev-1", StartedAt: t0,
			TitleSource: contract.SessionTitleSourceFirstPrompt, Sources: []contract.SessionSourcesElem{contract.SessionSourcesElemHook},
			TaskTitle: ptr("Fix /Users/alice/secret-project build using " + fakeToken() + " " + strings.Repeat("long ", 40)),
			Repo:      &contract.SessionRepo{PathHash: "0123456789abcdef", RemoteUrl: ptr(creds), DisplayName: ptr("/Users/alice/code/app")},
		}}},
		Resources: []contract.Resource{{
			ID: rid, Provider: "supabase", AccountID: "org-fake", Kind: "supabase.project", ProviderID: "abcdefghijklmnopqrst",
			Name: "demo", Existence: contract.ResourceExistenceConfirmed, SeenVia: contract.ResourceSeenViaList,
			FirstSeenAt: t0, LastSeenAt: t0, Url: ptr("https://example.supabase.co/?share=abc"),
			Attributes: contract.ResourceAttributes{"region": "us-east-1", "db_password": "pw-value", "public": true, "nested": map[string]any{"x": 1}},
		}},
		Attributions: []contract.Attribution{{ResourceID: rid, Label: contract.AttributionLabelExact, SessionID: ptr(sid), Reason: "id in result",
			ComputedAt: t0, Evidence: []contract.Evidence{{ActionID: "toolu_1", SessionID: sid, OccurredAt: t0, ToolName: "Bash",
				SignatureID: "sig", MatchedValue: "abcdefghijklmnopqrst", Excerpt: "cd /home/alice/app && supabase projects create " + strings.Repeat("x", 3000)}}}},
		Findings: []contract.ExposureFinding{{ID: "rls_disabled:" + rid, ResourceID: rid, RuleID: "rls_disabled", State: contract.ExposureFindingStateExposed,
			Severity: contract.ExposureFindingSeverityHigh, Title: "RLS off", Explanation: "x", Fix: contract.Fix{Kind: contract.FixKindSql, Text: "alter table t enable row level security;"},
			Signal: contract.ExposureFindingSignalOfficialAdvisor}},
		Costs:    []contract.CostRecord{{ResourceID: rid, Source: contract.CostRecordSourceUnknown, AsOf: t0, Basis: "no data"}},
		Reviews:  []contract.ReviewItem{{ResourceID: rid, DueAt: t0, State: contract.ReviewItemStatePending}},
		Checks:   []contract.ProviderCheck{{Provider: "supabase", AccountID: "org-fake", Kind: "supabase.project", Status: contract.ProviderCheckStatusOk, AttemptedAt: t0}},
		Warnings: []contract.Warning{{Code: "local_only", Message: "never synced"}},
	}
}

func TestBuildBatchSanitizes(t *testing.T) {
	b, _ := BuildBatch(sampleAnswer(), "1.2.3", nil)
	raw, _ := json.Marshal(b)
	s := string(raw)
	for _, bad := range []string{"/Users/alice", "/home/alice", fakeToken(), "hunter2", "pw-value", "share=abc", "#frag", "never synced", "nested"} {
		if strings.Contains(s, bad) {
			t.Errorf("batch contains %q", bad)
		}
	}
	sess := b.Sessions[0].Entity
	if n := len([]rune(*sess.TaskTitle)); n > 80 {
		t.Errorf("task title has %d runes", n)
	}
	if got := *sess.Repo.RemoteUrl; got != "https://github.com/example/app.git" {
		t.Errorf("remote url: %q", got)
	}
	if got := *sess.Repo.DisplayName; got != "app" {
		t.Errorf("repo display name: %q", got)
	}
	attrs := b.Resources[0].Entity.Attributes
	if attrs["db_password"] != "[REDACTED]" || attrs["region"] != "us-east-1" || attrs["public"] != true {
		t.Errorf("attributes: %v", attrs)
	}
	if n := len([]rune(b.Attributions[0].Entity.Evidence[0].Excerpt)); n > maxExcerpt {
		t.Errorf("excerpt has %d runes", n)
	}
	if b.ClientVersion != "1.2.3" || len(b.BatchID) != 32 || b.SchemaVersion != 1 {
		t.Errorf("batch header: %+v", b)
	}
}

func TestBuildBatchOnlyChanged(t *testing.T) {
	a := sampleAnswer()
	b1, revs := BuildBatch(a, "v", nil)
	if Empty(b1) || len(revs) != 7 {
		t.Fatalf("first batch: %d revs", len(revs))
	}
	for _, r := range revs {
		if len(r) != 64 {
			t.Fatalf("rev %q is not a hex sha256", r)
		}
	}
	b2, revs2 := BuildBatch(a, "v", revs)
	if !Empty(b2) || len(revs2) != 7 {
		t.Fatalf("unchanged answer produced a non-empty batch: %+v", b2)
	}
	a.Resources[0].Name = "renamed"
	b3, _ := BuildBatch(a, "v", revs2)
	if len(b3.Resources) != 1 || len(b3.Sessions)+len(b3.Findings)+len(b3.Checks) != 0 {
		t.Fatalf("one change: %d resources, %d sessions", len(b3.Resources), len(b3.Sessions))
	}
	// Arrays are never null (the schema requires them).
	raw, _ := json.Marshal(b2)
	if strings.Contains(string(raw), "null") {
		t.Fatalf("empty batch has nulls: %s", raw)
	}
}

func TestBaseURLMustBeHTTPS(t *testing.T) {
	for _, u := range []string{"http://sync.example.com", "ftp://x", "https://user:pw@x", "not a url"} {
		if err := Push(context.Background(), u, fakeToken(), contract.SyncBatch{}); err == nil || errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s: want URL error, got %v", u, err)
		}
	}
}

// waitForCode reads Login's output until it prints the user code.
func waitForCode(t *testing.T, r io.Reader) string {
	t.Helper()
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if _, code, ok := strings.Cut(sc.Text(), "shows the code:"); ok {
			go func() { _, _ = io.Copy(io.Discard, r) }()
			return strings.TrimSpace(code)
		}
	}
	t.Fatal("login printed no code")
	return ""
}

// TestDeviceFlowEndToEnd runs the CLI client against the real server: login
// through the device flow, approve on the "dashboard", push, read back.
func TestDeviceFlowEndToEnd(t *testing.T) {
	pollUnit = 40 * time.Millisecond
	defer func() { pollUnit = time.Second }()
	s := testserver.Start(t, testserver.Options{RealClock: true})
	alice := s.Login(t, "alice")

	pr, pw := io.Pipe()
	type result struct {
		token string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		tok, err := Login(context.Background(), s.URL, pw)
		_ = pw.Close()
		done <- result{tok, err}
	}()
	code := waitForCode(t, pr)
	if len(code) != 9 || code[4] != '-' {
		t.Fatalf("user code %q", code)
	}
	time.Sleep(100 * time.Millisecond) // a few authorization_pending polls first
	alice.Approve(code)
	var res result
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("login did not finish")
	}
	if res.err != nil || !strings.HasPrefix(res.token, "mrt_") {
		t.Fatalf("login: got token %v, err %v", res.token != "", res.err)
	}

	b, _ := BuildBatch(sampleAnswer(), "test", nil)
	if err := Push(context.Background(), s.URL, res.token, b); err != nil {
		t.Fatalf("push: %v", err)
	}
	var me contract.Answer
	alice.JSON("GET", "/v1/answer?scope=me", nil, 200, &me)
	if len(me.Sessions) != 1 || len(me.Resources) != 1 || me.Sessions[0].DeveloperName != "Alice" {
		t.Fatalf("server view after push: %d sessions %d resources", len(me.Sessions), len(me.Resources))
	}

	if err := Push(context.Background(), s.URL, fakeToken(), b); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("push with unknown token: %v", err)
	}
	bad := b
	bad.BatchID = "not-hex"
	var se *ServerError
	if err := Push(context.Background(), s.URL, res.token, bad); !errors.As(err, &se) || se.Status != 422 || len(se.Problems) == 0 {
		t.Fatalf("invalid batch: %v", err)
	}
	if err := Revoke(context.Background(), s.URL, res.token); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := Push(context.Background(), s.URL, res.token, b); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("push after revoke: %v", err)
	}
}

func TestLoginSlowDownAndDenied(t *testing.T) {
	pollUnit = 5 * time.Millisecond
	defer func() { pollUnit = time.Second }()
	var polls atomic.Int32
	var lastGap atomic.Int64
	var last atomic.Int64
	deny := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/device/code":
			_, _ = w.Write([]byte(`{"device_code":"dc","user_code":"BCDF-GHJK","verification_uri":"http://localhost:3000/device","interval":1,"expires_in":60}`))
		case "/v1/device/token":
			now := time.Now().UnixNano()
			if p := last.Swap(now); p != 0 {
				lastGap.Store(now - p)
			}
			n := polls.Add(1)
			switch {
			case deny.Load():
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"access_denied"}`))
			case n == 1:
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"slow_down","interval":6}`))
			case n < 3:
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
			default:
				_, _ = w.Write([]byte(`{"access_token":"` + fakeToken() + `","token_type":"bearer","login":"alice"}`))
			}
		}
	}))
	defer srv.Close()

	var out strings.Builder
	tok, err := Login(context.Background(), srv.URL, &out)
	if err != nil || tok != fakeToken() {
		t.Fatalf("login: %v", err)
	}
	// After slow_down the interval grew from 1 to 6 units.
	if gap := time.Duration(lastGap.Load()); gap < 6*pollUnit {
		t.Fatalf("poll gap after slow_down: %v", gap)
	}
	if !strings.Contains(out.String(), "BCDF-GHJK") || strings.Contains(out.String(), fakeToken()) {
		t.Fatalf("output: %q", out.String())
	}

	deny.Store(true)
	if _, err := Login(context.Background(), srv.URL, io.Discard); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("denied login: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Login(ctx, srv.URL, io.Discard); err == nil {
		t.Fatal("cancelled login succeeded")
	}
}
