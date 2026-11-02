package api_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/server/internal/api"
	"github.com/tolgayayci/muretto/server/internal/config"
	"github.com/tolgayayci/muretto/server/internal/fixture"
	"github.com/tolgayayci/muretto/server/internal/store/memstore"
	"github.com/tolgayayci/muretto/server/testserver"
)

func post(t *testing.T, url, token, contentType string, body []byte) (*testserver.Reply, []byte) {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return &testserver.Reply{StatusCode: resp.StatusCode, Header: resp.Header}, out
}

func get(t *testing.T, url, token string) (*testserver.Reply, []byte) {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return &testserver.Reply{StatusCode: resp.StatusCode, Header: resp.Header}, out
}

func ingest(t *testing.T, s *testserver.Server, token string, b contract.SyncBatch) (accepted, unchanged int) {
	t.Helper()
	resp, body := post(t, s.URL+"/v1/ingest", token, "application/json", fixture.JSON(b))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ingest: %d %s", resp.StatusCode, body)
	}
	var out struct{ Accepted, Unchanged int }
	_ = json.Unmarshal(body, &out)
	return out.Accepted, out.Unchanged
}

func TestIngestValidationAndIdempotency(t *testing.T) {
	s := testserver.Start(t, testserver.Options{MaxBody: 64 << 10})
	tok := s.Token(t, "alice")
	b := fixture.Batch("alice", "projaaaaaaaaaaaaaaaa")

	if a, u := ingest(t, s, tok, b); a != 7 || u != 0 {
		t.Fatalf("first ingest: accepted %d unchanged %d", a, u)
	}
	if a, u := ingest(t, s, tok, b); a != 0 || u != 7 {
		t.Fatalf("replayed ingest: accepted %d unchanged %d", a, u)
	}
	b2 := b
	b2.Resources = append([]contract.SyncResource(nil), b.Resources...)
	b2.Resources[0].Rev = contract.Rev(strings.Repeat("e", 64))
	b2.Resources[0].Entity.Name = "renamed"
	if a, u := ingest(t, s, tok, b2); a != 1 || u != 6 {
		t.Fatalf("changed rev: accepted %d unchanged %d", a, u)
	}

	good := string(fixture.JSON(b))
	cases := []struct {
		name, ct, body string
		status         int
		code           string
	}{
		{"unknown field", "application/json", strings.Replace(good, `"client_version"`, `"token":"x","client_version"`, 1), 422, "invalid_batch"},
		{"bad enum", "application/json", strings.Replace(good, `"existence":"confirmed"`, `"existence":"maybe"`, 1), 422, "invalid_batch"},
		{"bad date", "application/json", strings.Replace(good, `"sent_at":"2026-10-26T14:00:00Z"`, `"sent_at":"soon"`, 1), 422, "invalid_batch"},
		{"missing arrays", "application/json", `{"schema_version":1}`, 422, "invalid_batch"},
		{"not json", "application/json", `{`, 422, "invalid_batch"},
		{"wrong content type", "text/plain", good, 415, "unsupported_media_type"},
		{"too large", "application/json", `{"pad":"` + strings.Repeat("x", 70<<10) + `"}`, 413, "body_too_large"},
	}
	for _, c := range cases {
		resp, body := post(t, s.URL+"/v1/ingest", tok, c.ct, []byte(c.body))
		var e struct {
			Error     string   `json:"error"`
			Problems  []string `json:"problems"`
			RequestID string   `json:"request_id"`
		}
		_ = json.Unmarshal(body, &e)
		if resp.StatusCode != c.status || e.Error != c.code || e.RequestID == "" {
			t.Errorf("%s: got %d %s", c.name, resp.StatusCode, body)
		}
		if c.code == "invalid_batch" && len(e.Problems) == 0 {
			t.Errorf("%s: no problems listed", c.name)
		}
	}
}

func TestAuthFailures(t *testing.T) {
	s := testserver.Start(t, testserver.Options{})
	tok := s.Token(t, "alice")
	body := fixture.JSON(fixture.Batch("alice", "projaaaaaaaaaaaaaaaa"))

	for name, token := range map[string]string{"none": "", "malformed": "abc", "unknown": "mrt_" + strings.Repeat("a", 52), "jwt": s.SupabaseToken(t, "alice")} {
		resp, _ := post(t, s.URL+"/v1/ingest", token, "application/json", body)
		if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") == "" {
			t.Errorf("ingest with %s token: %d", name, resp.StatusCode)
		}
	}
	// CLI tokens work for "either" endpoints.
	if resp, _ := get(t, s.URL+"/v1/me", tok); resp.StatusCode != http.StatusOK {
		t.Fatalf("me with bearer: %d", resp.StatusCode)
	}
	// Other authorization schemes and cookies authenticate nothing.
	for name, set := range map[string]func(*http.Request){
		"basic": func(r *http.Request) { r.Header.Set("Authorization", "Basic "+tok) },
		"raw":   func(r *http.Request) { r.Header.Set("Authorization", tok) },
		"cookie": func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "mrt_session", Value: "mrs_" + strings.Repeat("a", 52)})
		},
	} {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, s.URL+"/v1/me", nil)
		set(req)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("me with %s: %d", name, resp.StatusCode)
		}
	}
	// A Supabase token cannot log a CLI out.
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete, s.URL+"/v1/tokens/self", nil)
	req.Header.Set("Authorization", "Bearer "+s.SupabaseToken(t, "alice"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoke self with a JWT: %v %v", resp, err)
	}
	_ = resp.Body.Close()

	// Revoked tokens stop working.
	req, _ = http.NewRequestWithContext(context.Background(), http.MethodDelete, s.URL+"/v1/tokens/self", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %v %v", resp, err)
	}
	_ = resp.Body.Close()
	if resp, _ := get(t, s.URL+"/v1/me", tok); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token: %d", resp.StatusCode)
	}

	// Strict decoding: unknown fields rejected.
	bob := s.Login(t, "bob")
	if resp, _ := bob.Do("POST", "/v1/orgs", map[string]string{"name": "x", "owner": "y"}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", resp.StatusCode)
	}
}

func TestSupabaseAuth(t *testing.T) {
	s := testserver.Start(t, testserver.Options{Prod: true})
	type me struct {
		Login       string `json:"login"`
		DisplayName string `json:"display_name"`
		Orgs        []struct{ ID string }
	}
	var m me
	resp, body := get(t, s.URL+"/v1/me", s.SupabaseToken(t, "octo"))
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &m) != nil || m.Login != "octo" || m.DisplayName != "Octo" {
		t.Fatalf("me: %d %s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "csrf") {
		t.Fatalf("me still returns a CSRF token: %s", body)
	}
	octo := s.Login(t, "octo")
	var org orgResp
	octo.JSON("POST", "/v1/orgs", map[string]string{"name": "Acme"}, 201, &org)

	// The account is keyed by sub. Changing user_metadata changes only what
	// is displayed; it never switches accounts.
	c := s.SupabaseClaims("octo")
	c["user_metadata"] = map[string]any{"user_name": "renamed\u0000", "full_name": "  New Name  ", "sub": testserver.SupabaseSubject("mallory")}
	resp, body = get(t, s.URL+"/v1/me", s.SignSupabase(t, c))
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &m) != nil || m.Login != "renamed" || m.DisplayName != "New Name" || len(m.Orgs) != 1 || m.Orgs[0].ID != org.ID {
		t.Fatalf("me after metadata change: %d %s", resp.StatusCode, body)
	}
	// Another user claiming octo's user_name gets their own, empty account.
	c = s.SupabaseClaims("mallory")
	c["user_metadata"] = map[string]any{"user_name": "octo"}
	resp, body = get(t, s.URL+"/v1/me", s.SignSupabase(t, c))
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &m) != nil || len(m.Orgs) != 0 {
		t.Fatalf("mallory: %d %s", resp.StatusCode, body)
	}
	if resp, _ := get(t, s.URL+"/v1/orgs/"+org.ID, s.SignSupabase(t, c)); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("mallory reads octo's org: %d", resp.StatusCode)
	}
	// No user_metadata: a placeholder login.
	c = s.SupabaseClaims("nometa")
	delete(c, "user_metadata")
	if resp, body = get(t, s.URL+"/v1/me", s.SignSupabase(t, c)); resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"login":"user-`) {
		t.Fatalf("no metadata: %d %s", resp.StatusCode, body)
	}

	// Rejections (the verifier's own tests cover every case; these check the
	// API wiring).
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	foreign := jwt.NewWithClaims(jwt.SigningMethodES256, s.SupabaseClaims("octo"))
	foreign.Header["kid"] = "test-key-1"
	foreignRaw, _ := foreign.SignedString(other)
	bad := map[string]string{
		"other key": foreignRaw,
		"garbage":   "eyJhbGciOiJub25lIn0.e30.",
	}
	for k, v := range map[string]any{"role": "anon", "aud": "anon", "iss": "https://evil.example/auth/v1", "exp": s.Clock.Now().Add(-time.Hour).Unix(), "sub": "not-a-uuid"} {
		c := s.SupabaseClaims("octo")
		c[k] = v
		bad[k] = s.SignSupabase(t, c)
	}
	for name, tok := range bad {
		if resp, _ := get(t, s.URL+"/v1/me", tok); resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") == "" {
			t.Errorf("%s: %d", name, resp.StatusCode)
		}
	}
	// Expiry follows the server clock.
	stale := s.SupabaseToken(t, "octo")
	s.Clock.Advance(2 * time.Hour)
	if resp, _ := get(t, s.URL+"/v1/me", stale); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired token: %d", resp.StatusCode)
	}
	if n := s.JWKSFetches(); n < 1 || n > 3 {
		t.Fatalf("JWKS fetched %d times", n)
	}
}

// In production a CLI token is not a dashboard sign-in: it cannot approve
// device logins (which would mint more tokens) or manage tokens or orgs.
func TestProdCLITokenCannotActAsDashboardUser(t *testing.T) {
	s := testserver.Start(t, testserver.Options{Prod: true})
	alice := s.Login(t, "alice")
	tok := s.DeviceToken(t, alice, "laptop")

	// The device flow worked end to end: the CLI token reads and writes.
	if resp, body := get(t, s.URL+"/v1/me", tok); resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"login":"alice"`) {
		t.Fatalf("me with CLI token: %d %s", resp.StatusCode, body)
	}
	ingest(t, s, tok, fixture.Batch("alice", "projaaaaaaaaaaaaaaaa"))
	var a contract.Answer
	alice.JSON("GET", "/v1/answer", nil, 200, &a)
	if len(a.Resources) == 0 {
		t.Fatal("dashboard user does not see the CLI's data")
	}
	if resp, _ := get(t, s.URL+"/v1/answer", tok); resp.StatusCode != http.StatusOK {
		t.Fatalf("answer with CLI token: %d", resp.StatusCode)
	}

	// A second CLI asks to log in; the stolen token tries to approve it.
	resp, body := post(t, s.URL+"/v1/device/code", "", "application/json", []byte(`{"host":"attacker"}`))
	var code struct {
		UserCode string `json:"user_code"`
	}
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &code) != nil {
		t.Fatalf("device code: %d", resp.StatusCode)
	}
	var tokens struct {
		Tokens []struct{ ID string } `json:"tokens"`
	}
	alice.JSON("GET", "/v1/tokens", nil, 200, &tokens)
	if len(tokens.Tokens) != 1 {
		t.Fatalf("tokens: %+v", tokens)
	}
	do := func(method, path, body string) int {
		req, _ := http.NewRequestWithContext(context.Background(), method, s.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/v1/device?user_code=" + url.QueryEscape(code.UserCode), ""},
		{"POST", "/v1/device/approve", `{"user_code":"` + code.UserCode + `"}`},
		{"GET", "/v1/tokens", ""},
		{"DELETE", "/v1/tokens/" + tokens.Tokens[0].ID, ""},
		{"POST", "/v1/orgs", `{"name":"x"}`},
		{"GET", "/v1/orgs/org_x", ""},
		{"POST", "/v1/invites/mri_x/accept", ""},
	} {
		if st := do(c.method, c.path, c.body); st != 403 {
			t.Errorf("%s %s with a CLI token: %d, want 403", c.method, c.path, st)
		}
	}
	// The code is still pending (not approved by the CLI token).
	var info struct{ Client struct{ Host string } }
	alice.JSON("GET", "/v1/device?user_code="+url.QueryEscape(code.UserCode), nil, 200, &info)
	if info.Client.Host != "attacker" {
		t.Fatalf("device info: %+v", info)
	}
	// The token was not revoked either.
	if resp, _ := get(t, s.URL+"/v1/me", tok); resp.StatusCode != http.StatusOK {
		t.Fatalf("token after refused revoke: %d", resp.StatusCode)
	}
	// No dev routes in production mode.
	if r, _ := post(t, s.URL+"/v1/dev/token", "", "application/json", []byte(`{"login":"alice"}`)); r.StatusCode != http.StatusNotFound && r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("dev token in prod: %d", r.StatusCode)
	}
}

// With dev login (loopback only), a CLI token stands in for a dashboard user
// so local development and the demo need no Supabase project.
func TestDevLoginCLITokenActsAsUser(t *testing.T) {
	s := testserver.Start(t, testserver.Options{})
	dev := s.DevToken(t, "dana")
	resp, body := post(t, s.URL+"/v1/orgs", dev, "application/json", []byte(`{"name":"Dev Org"}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create org with dev token: %d %s", resp.StatusCode, body)
	}
	if resp, body := get(t, s.URL+"/v1/tokens", dev); resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"name":"dev"`) {
		t.Fatalf("tokens with dev token: %d %s", resp.StatusCode, body)
	}
	// Approve a device code with the dev token; the CLI then gets a token.
	_, body = post(t, s.URL+"/v1/device/code", "", "application/json", []byte(`{"host":"box"}`))
	var code struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
	}
	_ = json.Unmarshal(body, &code)
	if resp, body := post(t, s.URL+"/v1/device/approve", dev, "application/json", []byte(`{"user_code":"`+code.UserCode+`"}`)); resp.StatusCode != http.StatusOK {
		t.Fatalf("approve with dev token: %d %s", resp.StatusCode, body)
	}
	resp, body = post(t, s.URL+"/v1/device/token", "", "application/json", []byte(`{"device_code":"`+code.DeviceCode+`"}`))
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"login":"dana"`) {
		t.Fatalf("device token: %d %s", resp.StatusCode, body)
	}
}

func TestTokensListAndRevoke(t *testing.T) {
	s := testserver.Start(t, testserver.Options{Prod: true})
	alice, bob := s.Login(t, "alice"), s.Login(t, "bob")
	laptop := s.DeviceToken(t, alice, "laptop")
	s.Clock.Advance(time.Second)
	desktop := s.DeviceToken(t, alice, "desktop")
	bobTok := s.DeviceToken(t, bob, "bobs-box")

	type tokenInfo struct {
		ID         string     `json:"id"`
		Name       string     `json:"name"`
		CreatedAt  time.Time  `json:"created_at"`
		LastUsedAt *time.Time `json:"last_used_at"`
	}
	list := func(b *testserver.Browser) []tokenInfo {
		t.Helper()
		resp, raw := b.Do("GET", "/v1/tokens", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list: %d %s", resp.StatusCode, raw)
		}
		for _, secret := range []string{laptop, desktop, bobTok, "hash", "token_hash"} {
			if strings.Contains(string(raw), secret) {
				t.Fatalf("token list leaks %q: %s", secret, raw)
			}
		}
		var out struct{ Tokens []tokenInfo }
		_ = json.Unmarshal(raw, &out)
		return out.Tokens
	}
	ts := list(alice)
	if len(ts) != 2 || ts[0].Name != "desktop" || ts[1].Name != "laptop" || ts[0].LastUsedAt != nil || ts[0].CreatedAt.IsZero() {
		t.Fatalf("alice's tokens: %+v", ts)
	}
	if bs := list(bob); len(bs) != 1 || bs[0].Name != "bobs-box" {
		t.Fatalf("bob's tokens: %+v", bs)
	}

	// last_used_at: set on use, written at most once a minute.
	used := s.Clock.Now().UTC().Truncate(time.Second)
	get(t, s.URL+"/v1/me", laptop)
	if lu := list(alice)[1].LastUsedAt; lu == nil || !lu.Equal(used) {
		t.Fatalf("last_used_at after use: %v want %v", lu, used)
	}
	s.Clock.Advance(30 * time.Second)
	get(t, s.URL+"/v1/me", laptop)
	if lu := list(alice)[1].LastUsedAt; lu == nil || !lu.Equal(used) {
		t.Fatalf("last_used_at moved within a minute: %v", lu)
	}
	s.Clock.Advance(31 * time.Second)
	get(t, s.URL+"/v1/me", laptop)
	if lu := list(alice)[1].LastUsedAt; lu == nil || !lu.After(used) {
		t.Fatalf("last_used_at after a minute: %v", lu)
	}

	// alice cannot revoke bob's token: same 404 as an unknown id.
	bobID := list(bob)[0].ID
	for _, id := range []string{bobID, "tok_00000000000000000000000000000000", "nope", "tok_" + strings.Repeat("a", 100)} {
		resp, body := alice.Do("DELETE", "/v1/tokens/"+id, nil)
		if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), `"not_found"`) {
			t.Fatalf("revoke %s: %d %s", id, resp.StatusCode, body)
		}
	}
	if resp, _ := get(t, s.URL+"/v1/me", bobTok); resp.StatusCode != http.StatusOK {
		t.Fatalf("bob's token after alice's attempt: %d", resp.StatusCode)
	}
	// alice revokes her laptop.
	laptopID := list(alice)[1].ID
	alice.JSON("DELETE", "/v1/tokens/"+laptopID, nil, 204, nil)
	if resp, _ := get(t, s.URL+"/v1/me", laptop); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked laptop token: %d", resp.StatusCode)
	}
	if resp, _ := alice.Do("DELETE", "/v1/tokens/"+laptopID, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second revoke: %d", resp.StatusCode)
	}
	if ts := list(alice); len(ts) != 1 || ts[0].Name != "desktop" {
		t.Fatalf("after revoke: %+v", ts)
	}
	if resp, _ := get(t, s.URL+"/v1/me", desktop); resp.StatusCode != http.StatusOK {
		t.Fatalf("other token after revoke: %d", resp.StatusCode)
	}
}

type orgResp struct {
	ID      string `json:"id"`
	Members []struct {
		AccountLogin string `json:"account_login"`
		Role         string `json:"role"`
	} `json:"members"`
}

func TestTeamAnswerAndInvites(t *testing.T) {
	s := testserver.Start(t, testserver.Options{})
	alice, bob, carol := s.Login(t, "alice"), s.Login(t, "bob"), s.Login(t, "carol")
	ingest(t, s, s.Token(t, "alice"), fixture.Batch("alice", "projaaaaaaaaaaaaaaaa"))
	ingest(t, s, s.Token(t, "bob"), fixture.Batch("bob", "projbbbbbbbbbbbbbbbb"))

	var org orgResp
	alice.JSON("POST", "/v1/orgs", map[string]string{"name": "Acme"}, 201, &org)
	var inv struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	alice.JSON("POST", "/v1/orgs/"+org.ID+"/invites", nil, 201, &inv)
	if !strings.HasPrefix(inv.URL, "http://localhost:3000/join/mri_") || !inv.ExpiresAt.After(s.Clock.Now()) {
		t.Fatalf("invite: %+v", inv)
	}
	token := inv.URL[strings.LastIndex(inv.URL, "/")+1:]

	// Before joining, bob cannot see the org.
	if resp, _ := bob.Do("GET", "/v1/orgs/"+org.ID+"/answer", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("non-member team answer: %d", resp.StatusCode)
	}
	var joined struct{ ID, Role string }
	bob.JSON("POST", "/v1/invites/"+token+"/accept", nil, 200, &joined)
	if joined.ID != org.ID || joined.Role != "member" {
		t.Fatalf("join: %+v", joined)
	}
	// Single use: carol cannot reuse it; bob re-accepting is also refused.
	if resp, _ := carol.Do("POST", "/v1/invites/"+token+"/accept", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("invite reused: %d", resp.StatusCode)
	}
	// Members cannot create invites.
	if resp, _ := bob.Do("POST", "/v1/orgs/"+org.ID+"/invites", nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member invite: %d", resp.StatusCode)
	}
	// Expired invites are refused.
	alice.JSON("POST", "/v1/orgs/"+org.ID+"/invites", nil, 201, &inv)
	s.Clock.Advance(8 * 24 * time.Hour)
	expired := inv.URL[strings.LastIndex(inv.URL, "/")+1:]
	if resp, _ := carol.Do("POST", "/v1/invites/"+expired+"/accept", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expired invite: %d", resp.StatusCode)
	}

	var team contract.Answer
	bob.JSON("GET", "/v1/orgs/"+org.ID+"/answer", nil, 200, &team)
	names := map[string]bool{}
	for _, sv := range team.Sessions {
		names[sv.DeveloperName] = true
	}
	if len(team.Sessions) != 2 || !names["Alice"] || !names["Bob"] || len(team.Resources) != 2 || team.Summary.Exposed != 2 {
		t.Fatalf("team answer: names %v sessions %d resources %d", names, len(team.Sessions), len(team.Resources))
	}

	var me contract.Answer
	bob.JSON("GET", "/v1/answer?scope=me", nil, 200, &me)
	if len(me.Sessions) != 1 || me.Sessions[0].DeveloperName != "Bob" || me.Developer.AccountLogin == nil || *me.Developer.AccountLogin != "bob" {
		t.Fatalf("me answer: %+v", me.Sessions)
	}
	if resp, _ := bob.Do("GET", "/v1/answer?scope=team", nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad scope: %d", resp.StatusCode)
	}

	var o orgResp
	bob.JSON("GET", "/v1/orgs/"+org.ID, nil, 200, &o)
	if len(o.Members) != 2 || o.Members[0].Role != "owner" {
		t.Fatalf("org members: %+v", o.Members)
	}
	var meResp struct {
		Login string
		Orgs  []struct{ ID, Name, Role string }
	}
	bob.JSON("GET", "/v1/me", nil, 200, &meResp)
	if meResp.Login != "bob" || len(meResp.Orgs) != 1 || meResp.Orgs[0].Role != "member" {
		t.Fatalf("me: %+v", meResp)
	}
}

func TestSSEEventAfterIngest(t *testing.T) {
	s := testserver.Start(t, testserver.Options{Heartbeat: 50 * time.Millisecond})
	alice := s.Login(t, "alice")
	var org orgResp
	alice.JSON("POST", "/v1/orgs", map[string]string{"name": "Acme"}, 201, &org)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	open := func(lastID string) *bufio.Scanner {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.URL+"/v1/orgs/"+org.ID+"/events", nil)
		req.Header.Set("Authorization", "Bearer "+alice.AccessToken())
		if lastID != "" {
			req.Header.Set("Last-Event-ID", lastID)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("events: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		return bufio.NewScanner(resp.Body)
	}
	type ev struct {
		Seq       int64  `json:"seq"`
		Type      string `json:"type"`
		ID        string `json:"id"`
		Developer string `json:"developer"`
	}
	next := func(sc *bufio.Scanner) (string, ev, bool) {
		var id string
		sawPing := false
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == ": ping":
				sawPing = true
			case strings.HasPrefix(line, "id: "):
				id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "data: "):
				var e ev
				_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e)
				return id, e, sawPing
			}
		}
		t.Fatalf("stream ended: %v", sc.Err())
		return "", ev{}, false
	}

	sc := open("")
	for s.Hub.Subscribers() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(120 * time.Millisecond) // let a heartbeat through
	ingest(t, s, s.Token(t, "alice"), fixture.Batch("alice", "projaaaaaaaaaaaaaaaa"))
	id, e, sawPing := next(sc)
	if !sawPing {
		t.Error("no heartbeat before the first event")
	}
	if e.Type != "session" || e.Developer != "alice" || id == "" {
		t.Fatalf("first event: id %q %+v", id, e)
	}
	seen := map[string]bool{e.Type: true}
	for range 6 {
		_, e, _ = next(sc)
		seen[e.Type] = true
	}
	if !seen["resource"] || !seen["finding"] {
		t.Fatalf("event types: %v", seen)
	}

	// Replay: reconnecting with Last-Event-ID gets what came after it.
	sc2 := open(id)
	_, e2, _ := next(sc2)
	if e2.Seq <= e.Seq-6 || e2.Seq == 0 {
		t.Fatalf("replay started at %+v after id %s", e2, id)
	}
	// Non-members get 404.
	bob := s.Login(t, "bob")
	if resp, _ := bob.Do("GET", "/v1/orgs/"+org.ID+"/events", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("non-member events: %d", resp.StatusCode)
	}
}

func TestRateLimits(t *testing.T) {
	s := testserver.Start(t, testserver.Options{IngestPerMinute: 2})
	tok := s.Token(t, "alice")
	b := fixture.Batch("alice", "projaaaaaaaaaaaaaaaa")
	ingest(t, s, tok, b)
	ingest(t, s, tok, b)
	resp, _ := post(t, s.URL+"/v1/ingest", tok, "application/json", fixture.JSON(b))
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("ingest over limit: %d", resp.StatusCode)
	}
	// Another token has its own bucket.
	ingest(t, s, s.Token(t, "bob"), fixture.Batch("bob", "projbbbbbbbbbbbbbbbb"))
	s.Clock.Advance(time.Minute)
	ingest(t, s, tok, b)

	for i := range 3 {
		resp, body := post(t, s.URL+"/v1/provider-requests", "", "application/json", []byte(`{"provider":"Neon","email":"dev@example.com"}`))
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("provider request %d: %d %s", i, resp.StatusCode, body)
		}
	}
	resp, _ = post(t, s.URL+"/v1/provider-requests", "", "application/json", []byte(`{"provider":"Neon"}`))
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("provider request over limit: %d", resp.StatusCode)
	}
	if n := len(s.Store.ProviderRequests()); n != 3 {
		t.Fatalf("stored %d provider requests", n)
	}
}

func TestProviderRequestSignedIn(t *testing.T) {
	s := testserver.Start(t, testserver.Options{})
	// The dashboard sends every user's request from one address: the per-IP
	// limit must not apply to signed-in users, and each user has their own.
	for _, login := range []string{"alice", "bob"} {
		for i := range 3 {
			resp, body := post(t, s.URL+"/v1/provider-requests", s.SupabaseToken(t, login), "application/json", []byte(`{"provider":"Railway"}`))
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("%s request %d: %d %s", login, i, resp.StatusCode, body)
			}
		}
	}
	resp, _ := post(t, s.URL+"/v1/provider-requests", s.SupabaseToken(t, "alice"), "application/json", []byte(`{"provider":"Railway"}`))
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("alice over limit: %d", resp.StatusCode)
	}
	if resp, _ := post(t, s.URL+"/v1/provider-requests", "", "application/json", []byte(`{"provider":"Neon"}`)); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("anonymous after signed-in requests: %d", resp.StatusCode)
	}
	if resp, _ := post(t, s.URL+"/v1/provider-requests", "mrt_"+strings.Repeat("a", 52), "application/json", []byte(`{"provider":"Neon"}`)); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token: %d", resp.StatusCode)
	}
	reqs := s.Store.ProviderRequests()
	if len(reqs) != 7 || reqs[0].UserID == "" || reqs[0].UserID == reqs[3].UserID || reqs[6].UserID != "" {
		t.Fatalf("stored: %+v", reqs)
	}
}

func TestProviderRequestValidation(t *testing.T) {
	s := testserver.Start(t, testserver.Options{})
	for _, body := range []string{`{"provider":""}`, `{"provider":"<script>"}`, `{"provider":"Neon","email":"nope"}`, `{"provider":"Neon","extra":1}`} {
		s.Clock.Advance(time.Hour) // stay under the per-IP limit
		if resp, _ := post(t, s.URL+"/v1/provider-requests", "", "application/json", []byte(body)); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d", body, resp.StatusCode)
		}
	}
}

func TestDeviceFlowServerSide(t *testing.T) {
	s := testserver.Start(t, testserver.Options{DevicePollInterval: 5 * time.Second})
	var code struct {
		DeviceCode, UserCode, VerificationURIComplete string `json:"-"`
	}
	resp, body := post(t, s.URL+"/v1/device/code", "", "application/json", []byte(`{"client":"muretto-cli","host":"laptop","os":"darwin"}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("device code: %d %s", resp.StatusCode, body)
	}
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	code.DeviceCode, code.UserCode, code.VerificationURIComplete = raw["device_code"].(string), raw["user_code"].(string), raw["verification_uri_complete"].(string)
	if raw["interval"].(float64) != 5 || len(code.UserCode) != 9 || !strings.Contains(code.VerificationURIComplete, "/device?code=") {
		t.Fatalf("device code response: %v", raw)
	}
	poll := func() map[string]any {
		_, b := post(t, s.URL+"/v1/device/token", "", "application/json", []byte(`{"device_code":"`+code.DeviceCode+`","grant_type":"urn:ietf:params:oauth:grant-type:device_code"}`))
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		return m
	}
	if m := poll(); m["error"] != "authorization_pending" {
		t.Fatalf("first poll: %v", m)
	}
	if m := poll(); m["error"] != "slow_down" || m["interval"].(float64) != 10 {
		t.Fatalf("fast poll: %v", m)
	}
	alice := s.Login(t, "alice")
	var info struct {
		Client struct{ Host string }
	}
	alice.JSON("GET", "/v1/device?user_code="+url.QueryEscape(strings.ToLower(code.UserCode)), nil, 200, &info)
	if info.Client.Host != "laptop" {
		t.Fatalf("device info: %+v", info)
	}
	alice.JSON("POST", "/v1/device/approve", map[string]any{"user_code": code.UserCode, "deny": true}, 200, nil)
	s.Clock.Advance(11 * time.Second)
	if m := poll(); m["error"] != "access_denied" {
		t.Fatalf("denied poll: %v", m)
	}
	s.Clock.Advance(20 * time.Second)
	if m := poll(); m["error"] != "expired_token" {
		t.Fatalf("poll after denial: %v", m)
	}
	if m := map[string]any{}; true {
		_, b := post(t, s.URL+"/v1/device/token", "", "application/json", []byte(`{"device_code":"nope"}`))
		_ = json.Unmarshal(b, &m)
		if m["error"] != "expired_token" {
			t.Fatalf("unknown code: %v", m)
		}
	}
	if resp, _ := alice.Do("POST", "/v1/device/approve", map[string]any{"user_code": "BCDF-GHJK"}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("approve unknown code: %d", resp.StatusCode)
	}
}

func TestCORSAndDevRoutes(t *testing.T) {
	s := testserver.Start(t, testserver.Options{})
	pre := func(origin string) *testserver.Reply {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodOptions, s.URL+"/v1/orgs", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return &testserver.Reply{StatusCode: resp.StatusCode, Header: resp.Header}
	}
	if r := pre("http://localhost:3000"); r.StatusCode != http.StatusNoContent || r.Header.Get("Access-Control-Allow-Origin") != "http://localhost:3000" ||
		r.Header.Get("Access-Control-Allow-Credentials") != "" || strings.Contains(r.Header.Get("Access-Control-Allow-Headers"), "CSRF") {
		t.Fatalf("allowed preflight: %d %v", r.StatusCode, r.Header)
	}
	if r := pre("https://evil.example"); r.StatusCode != http.StatusForbidden || r.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("foreign preflight: %d", r.StatusCode)
	}
	if r, _ := get(t, s.URL+"/healthz", ""); r.Header.Get("X-Request-Id") == "" {
		t.Fatal("no request id")
	}

	// Without MURETTO_DEV_LOGIN the dev routes do not exist.
	srv, err := api.New(api.Options{Config: config.Config{PublicURL: "http://127.0.0.1:1", DashboardURL: "http://localhost:3000", MaxBody: 1 << 20, IngestPerMinute: 10},
		Store: memstore.New(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	// The retired cookie login routes are gone in every mode.
	for _, base := range []string{ts.URL, s.URL} {
		for _, path := range []string{"/auth/dev/login?login=alice", "/auth/github/login", "/auth/github/callback"} {
			if r, _ := get(t, base+path, ""); r.StatusCode != http.StatusNotFound {
				t.Errorf("%s: %d", path, r.StatusCode)
			}
		}
		if r, _ := post(t, base+"/auth/logout", "", "", nil); r.StatusCode != http.StatusNotFound {
			t.Errorf("logout: %d", r.StatusCode)
		}
	}
	if r, _ := post(t, ts.URL+"/v1/dev/token", "", "application/json", []byte(`{"login":"alice"}`)); r.StatusCode != http.StatusNotFound && r.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("dev token without flag: %d", r.StatusCode)
	}
	// Without a Supabase project configured, JWTs are refused.
	if r, _ := get(t, ts.URL+"/v1/me", s.SupabaseToken(t, "alice")); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("JWT without Supabase configured: %d", r.StatusCode)
	}
}
