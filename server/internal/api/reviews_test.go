package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/server/internal/fixture"
	"github.com/tolgayayci/muretto/server/testserver"
)

const reviewRes = "supabase.project:projaaaaaaaaaaaaaaaa"

func reviewOf(t *testing.T, b *testserver.Browser) (contract.ReviewItem, int) {
	t.Helper()
	var a contract.Answer
	b.JSON("GET", "/v1/answer", nil, 200, &a)
	if len(a.Reviews) != 1 {
		t.Fatalf("reviews: %+v", a.Reviews)
	}
	return a.Reviews[0], a.Summary.NeedsReview
}

type decisionsResp struct {
	Decisions []struct {
		ResourceID string    `json:"resource_id"`
		State      string    `json:"state"`
		Note       *string   `json:"note"`
		DecidedAt  time.Time `json:"decided_at"`
	} `json:"decisions"`
}

func TestReviewDecisionsFromDashboard(t *testing.T) {
	s := testserver.Start(t, testserver.Options{Prod: true})
	alice, bob := s.Login(t, "alice"), s.Login(t, "bob")
	aliceTok := s.DeviceToken(t, alice, "laptop")
	ingest(t, s, aliceTok, fixture.Batch("alice", "projaaaaaaaaaaaaaaaa")) // pending, past due
	ingest(t, s, s.DeviceToken(t, bob, "box"), fixture.Batch("bob", "projaaaaaaaaaaaaaaaa"))

	if r, n := reviewOf(t, alice); r.State != contract.ReviewItemStatePending || n != 1 {
		t.Fatalf("before: %+v %d", r, n)
	}
	s.Clock.Advance(time.Minute)
	alice.JSON("POST", "/v1/reviews", map[string]any{"resource_id": reviewRes, "state": "keep", "note": " still demoing \r\n"}, 204, nil)
	r, n := reviewOf(t, alice)
	if r.State != contract.ReviewItemStateKeep || n != 0 || r.DecidedBy == nil || r.DecidedAt == nil || r.Note == nil || *r.Note != "still demoing" {
		t.Fatalf("after keep: %+v needs %d", r, n)
	}
	var me struct{ Login string }
	alice.JSON("GET", "/v1/me", nil, 200, &me)

	// Isolation: bob's view and decisions are untouched.
	if r, n := reviewOf(t, bob); r.State != contract.ReviewItemStatePending || n != 1 {
		t.Fatalf("alice's decision changed bob's view: %+v %d", r, n)
	}
	var bd decisionsResp
	bob.JSON("GET", "/v1/reviews", nil, 200, &bd)
	if len(bd.Decisions) != 0 {
		t.Fatalf("bob sees decisions: %+v", bd)
	}

	// GET /v1/reviews: the caller's own, also with the CLI token.
	resp, body := get(t, s.URL+"/v1/reviews", aliceTok)
	var ad decisionsResp
	if resp.StatusCode != 200 || json.Unmarshal(body, &ad) != nil || len(ad.Decisions) != 1 || ad.Decisions[0].State != "keep" ||
		ad.Decisions[0].ResourceID != reviewRes || ad.Decisions[0].Note == nil || ad.Decisions[0].DecidedAt.IsZero() {
		t.Fatalf("GET /v1/reviews with CLI token: %d %s", resp.StatusCode, body)
	}

	// Undo.
	s.Clock.Advance(time.Minute)
	alice.JSON("POST", "/v1/reviews", map[string]any{"resource_id": reviewRes, "state": "pending"}, 204, nil)
	if r, n := reviewOf(t, alice); r.State != contract.ReviewItemStatePending || n != 1 || r.DecidedAt != nil || r.DecidedBy != nil || r.Note != nil {
		t.Fatalf("after undo: %+v %d", r, n)
	}

	// A newer CLI decision beats the older dashboard undo.
	s.Clock.Advance(time.Minute)
	b := fixture.Batch("alice", "projaaaaaaaaaaaaaaaa")
	cliAt := s.Clock.Now().UTC().Truncate(time.Second)
	by := "dev-alice"
	b.Reviews[0].Entity.State, b.Reviews[0].Entity.DecidedAt, b.Reviews[0].Entity.DecidedBy = contract.ReviewItemStateNotNeeded, &cliAt, &by
	b.Reviews[0].Rev = contract.Rev(strings.Repeat("c", 64))
	ingest(t, s, aliceTok, b)
	if r, _ := reviewOf(t, alice); r.State != contract.ReviewItemStateNotNeeded || *r.DecidedBy != "dev-alice" {
		t.Fatalf("newer CLI decision: %+v", r)
	}
	// And a newer dashboard decision beats that.
	s.Clock.Advance(time.Minute)
	alice.JSON("POST", "/v1/reviews", map[string]any{"resource_id": reviewRes, "state": "keep"}, 204, nil)
	if r, _ := reviewOf(t, alice); r.State != contract.ReviewItemStateKeep || r.Note != nil {
		t.Fatalf("newer dashboard decision: %+v", r)
	}

	// A decision for an unknown resource is stored and has no effect.
	alice.JSON("POST", "/v1/reviews", map[string]any{"resource_id": "vercel.project:prj_unknown", "state": "not_needed"}, 204, nil)
	if r, _ := reviewOf(t, alice); r.State != contract.ReviewItemStateKeep {
		t.Fatalf("unknown resource decision changed the view: %+v", r)
	}
	alice.JSON("GET", "/v1/reviews", nil, 200, &ad)
	if len(ad.Decisions) != 2 {
		t.Fatalf("decisions: %+v", ad)
	}
}

func TestReviewDecisionValidationAndAuth(t *testing.T) {
	s := testserver.Start(t, testserver.Options{Prod: true})
	alice := s.Login(t, "alice")
	tok := s.DeviceToken(t, alice, "laptop")
	bad := []struct {
		name string
		body string
		code string
	}{
		{"missing resource", `{"state":"keep"}`, "invalid_resource_id"},
		{"no kind", `{"resource_id":"projaaaa","state":"keep"}`, "invalid_resource_id"},
		{"no provider", `{"resource_id":"project:projaaaa","state":"keep"}`, "invalid_resource_id"},
		{"empty provider id", `{"resource_id":"supabase.project:","state":"keep"}`, "invalid_resource_id"},
		{"space", `{"resource_id":"supabase.project:a b","state":"keep"}`, "invalid_resource_id"},
		{"control char", `{"resource_id":"supabase.project:a\u0007","state":"keep"}`, "invalid_resource_id"},
		{"upper-case kind", `{"resource_id":"Supabase.project:a","state":"keep"}`, "invalid_resource_id"},
		{"too long", `{"resource_id":"supabase.project:` + strings.Repeat("a", 500) + `","state":"keep"}`, "invalid_resource_id"},
		{"bad state", `{"resource_id":"` + reviewRes + `","state":"delete"}`, "invalid_state"},
		{"missing state", `{"resource_id":"` + reviewRes + `"}`, "invalid_state"},
		{"long note", `{"resource_id":"` + reviewRes + `","state":"keep","note":"` + strings.Repeat("é", 501) + `"}`, "invalid_note"},
		{"note control char", `{"resource_id":"` + reviewRes + `","state":"keep","note":"a\u0000b"}`, "invalid_note"},
		{"unknown field", `{"resource_id":"` + reviewRes + `","state":"keep","decided_by":"usr_x"}`, "invalid_json"},
		{"two values", `{"resource_id":"` + reviewRes + `","state":"keep"}{}`, "invalid_json"},
		{"note not a string", `{"resource_id":"` + reviewRes + `","state":"keep","note":1}`, "invalid_json"},
	}
	for _, c := range bad {
		resp, body := post(t, s.URL+"/v1/reviews", alice.AccessToken(), "application/json", []byte(c.body))
		if resp.StatusCode != 400 || !strings.Contains(string(body), `"`+c.code+`"`) {
			t.Errorf("%s: %d %s", c.name, resp.StatusCode, body)
		}
	}
	good := []byte(`{"resource_id":"vercel.env_var:prj_x/API_KEY","state":"not_needed","note":"line one\nline two"}`)
	if resp, body := post(t, s.URL+"/v1/reviews", alice.AccessToken(), "application/json", good); resp.StatusCode != 204 {
		t.Fatalf("good body: %d %s", resp.StatusCode, body)
	}
	if resp, _ := post(t, s.URL+"/v1/reviews", alice.AccessToken(), "text/plain", good); resp.StatusCode != 415 {
		t.Fatalf("content type: %d", resp.StatusCode)
	}
	// Auth matrix: in production a CLI token cannot decide; no token at all is 401.
	if resp, _ := post(t, s.URL+"/v1/reviews", tok, "application/json", good); resp.StatusCode != 403 {
		t.Fatalf("POST with CLI token in prod: %d", resp.StatusCode)
	}
	if resp, _ := post(t, s.URL+"/v1/reviews", "", "application/json", good); resp.StatusCode != 401 {
		t.Fatalf("POST without auth: %d", resp.StatusCode)
	}
	if resp, _ := get(t, s.URL+"/v1/reviews", ""); resp.StatusCode != 401 {
		t.Fatalf("GET without auth: %d", resp.StatusCode)
	}

	// Rate limit per user (burst 30); another user is unaffected.
	limited := false
	for range 40 {
		resp, _ := post(t, s.URL+"/v1/reviews", alice.AccessToken(), "application/json", good)
		if resp.StatusCode == http.StatusTooManyRequests {
			limited = resp.Header.Get("Retry-After") != ""
			break
		}
	}
	if !limited {
		t.Fatal("no rate limit on POST /v1/reviews")
	}
	bob := s.Login(t, "bob")
	if resp, _ := post(t, s.URL+"/v1/reviews", bob.AccessToken(), "application/json", good); resp.StatusCode != 204 {
		t.Fatalf("bob limited by alice: %d", resp.StatusCode)
	}

	// Dev login: a CLI token may decide (local dev and the demo).
	dev := testserver.Start(t, testserver.Options{})
	if resp, body := post(t, dev.URL+"/v1/reviews", dev.DevToken(t, "dana"), "application/json", good); resp.StatusCode != 204 {
		t.Fatalf("dev token POST: %d %s", resp.StatusCode, body)
	}
}
