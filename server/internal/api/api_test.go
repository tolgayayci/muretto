package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/server/internal/api"
	"github.com/tolgayayci/muretto/server/internal/config"
	"github.com/tolgayayci/muretto/server/internal/fixture"
	"github.com/tolgayayci/muretto/server/internal/store/memstore"
	"github.com/tolgayayci/muretto/server/testserver"
)

func post(t *testing.T, url, token, contentType string, body []byte) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
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
	return resp, out
}

func get(t *testing.T, url, token string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp, out
}

func ingest(t *testing.T, s *testserver.Server, token string, b contract.SyncBatch) (accepted, unchanged int) {
	t.Helper()
	resp, body := post(t, s.URL+"/v1/ingest", token, "application/json", fixture.JSON(b))
	if resp.StatusCode != 200 {
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

	for name, token := range map[string]string{"none": "", "malformed": "abc", "unknown": "mrt_" + strings.Repeat("a", 52)} {
		resp, _ := post(t, s.URL+"/v1/ingest", token, "application/json", body)
		if resp.StatusCode != 401 || resp.Header.Get("WWW-Authenticate") == "" {
			t.Errorf("ingest with %s token: %d", name, resp.StatusCode)
		}
	}
	// Bearer tokens work for "either" endpoints, not cookie-only ones.
	if resp, _ := get(t, s.URL+"/v1/me", tok); resp.StatusCode != 200 {
		t.Fatalf("me with bearer: %d", resp.StatusCode)
	}
	if resp, _ := post(t, s.URL+"/v1/orgs", tok, "application/json", []byte(`{"name":"x"}`)); resp.StatusCode != 401 {
		t.Fatalf("cookie endpoint with bearer: %d", resp.StatusCode)
	}

	// Cookie POSTs need the CSRF token.
	alice := s.Login(t, "alice")
	csrf := alice.CSRF
	alice.CSRF = ""
	if resp, _ := alice.Do("POST", "/v1/orgs", map[string]string{"name": "Acme"}); resp.StatusCode != 403 {
		t.Fatalf("POST without CSRF: %d", resp.StatusCode)
	}
	alice.CSRF = "0" + csrf[1:]
	if resp, _ := alice.Do("POST", "/v1/orgs", map[string]string{"name": "Acme"}); resp.StatusCode != 403 {
		t.Fatalf("POST with wrong CSRF: %d", resp.StatusCode)
	}
	alice.CSRF = csrf
	alice.JSON("POST", "/v1/orgs", map[string]string{"name": "Acme"}, 201, nil)

	// A cookie never authenticates ingest.
	if resp, _ := alice.Do("POST", "/v1/ingest", json.RawMessage(body)); resp.StatusCode != 401 {
		t.Fatalf("ingest with cookie: %d", resp.StatusCode)
	}

	// Revoked tokens stop working.
	req, _ := http.NewRequest("DELETE", s.URL+"/v1/tokens/self", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 204 {
		t.Fatalf("revoke: %v %v", resp, err)
	}
	_ = resp.Body.Close()
	if resp, _ := get(t, s.URL+"/v1/me", tok); resp.StatusCode != 401 {
		t.Fatalf("revoked token: %d", resp.StatusCode)
	}

	// Logout ends the session.
	alice.JSON("POST", "/auth/logout", nil, 204, nil)
	if resp, _ := alice.Do("GET", "/v1/me", nil); resp.StatusCode != 401 {
		t.Fatalf("after logout: %d", resp.StatusCode)
	}
	// Strict decoding: unknown fields rejected.
	bob := s.Login(t, "bob")
	if resp, _ := bob.Do("POST", "/v1/orgs", map[string]string{"name": "x", "owner": "y"}); resp.StatusCode != 400 {
		t.Fatalf("unknown field: %d", resp.StatusCode)
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
	if resp, _ := bob.Do("GET", "/v1/orgs/"+org.ID+"/answer", nil); resp.StatusCode != 404 {
		t.Fatalf("non-member team answer: %d", resp.StatusCode)
	}
	var joined struct{ ID, Role string }
	bob.JSON("POST", "/v1/invites/"+token+"/accept", nil, 200, &joined)
	if joined.ID != org.ID || joined.Role != "member" {
		t.Fatalf("join: %+v", joined)
	}
	// Single use: carol cannot reuse it; bob re-accepting is also refused.
	if resp, _ := carol.Do("POST", "/v1/invites/"+token+"/accept", nil); resp.StatusCode != 404 {
		t.Fatalf("invite reused: %d", resp.StatusCode)
	}
	// Members cannot create invites.
	if resp, _ := bob.Do("POST", "/v1/orgs/"+org.ID+"/invites", nil); resp.StatusCode != 403 {
		t.Fatalf("member invite: %d", resp.StatusCode)
	}
	// Expired invites are refused.
	alice.JSON("POST", "/v1/orgs/"+org.ID+"/invites", nil, 201, &inv)
	s.Clock.Advance(8 * 24 * time.Hour)
	expired := inv.URL[strings.LastIndex(inv.URL, "/")+1:]
	if resp, _ := carol.Do("POST", "/v1/invites/"+expired+"/accept", nil); resp.StatusCode != 404 {
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
	if resp, _ := bob.Do("GET", "/v1/answer?scope=team", nil); resp.StatusCode != 400 {
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
		req, _ := http.NewRequestWithContext(ctx, "GET", s.URL+"/v1/orgs/"+org.ID+"/events", nil)
		req.AddCookie(&http.Cookie{Name: "mrt_session", Value: alice.Cookie})
		if lastID != "" {
			req.Header.Set("Last-Event-ID", lastID)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
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
	if resp, _ := bob.Do("GET", "/v1/orgs/"+org.ID+"/events", nil); resp.StatusCode != 404 {
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
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("ingest over limit: %d", resp.StatusCode)
	}
	// Another token has its own bucket.
	ingest(t, s, s.Token(t, "bob"), fixture.Batch("bob", "projbbbbbbbbbbbbbbbb"))
	s.Clock.Advance(time.Minute)
	ingest(t, s, tok, b)

	for i := range 3 {
		resp, body := post(t, s.URL+"/v1/provider-requests", "", "application/json", []byte(`{"provider":"Neon","email":"dev@example.com"}`))
		if resp.StatusCode != 202 {
			t.Fatalf("provider request %d: %d %s", i, resp.StatusCode, body)
		}
	}
	resp, _ = post(t, s.URL+"/v1/provider-requests", "", "application/json", []byte(`{"provider":"Neon"}`))
	if resp.StatusCode != 429 {
		t.Fatalf("provider request over limit: %d", resp.StatusCode)
	}
	if n := s.Store.ProviderRequests(); n != 3 {
		t.Fatalf("stored %d provider requests", n)
	}
}

func TestProviderRequestValidation(t *testing.T) {
	s := testserver.Start(t, testserver.Options{})
	for _, body := range []string{`{"provider":""}`, `{"provider":"<script>"}`, `{"provider":"Neon","email":"nope"}`, `{"provider":"Neon","extra":1}`} {
		s.Clock.Advance(time.Hour) // stay under the per-IP limit
		if resp, _ := post(t, s.URL+"/v1/provider-requests", "", "application/json", []byte(body)); resp.StatusCode != 400 {
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
	if resp.StatusCode != 200 {
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
	if resp, _ := alice.Do("POST", "/v1/device/approve", map[string]any{"user_code": "BCDF-GHJK"}); resp.StatusCode != 404 {
		t.Fatalf("approve unknown code: %d", resp.StatusCode)
	}
}

func TestGitHubOAuth(t *testing.T) {
	var gotVerifier, gotToken string
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = r.ParseForm()
			gotVerifier = r.PostForm.Get("code_verifier")
			if r.PostForm.Get("code") != "good-code" || r.PostForm.Get("client_id") != "test-client-id" {
				w.WriteHeader(400)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"fake-github-access","token_type":"bearer"}`))
		case "/user":
			gotToken = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"id":4242,"login":"octo","name":"Octo Cat"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer gh.Close()
	s := testserver.Start(t, testserver.Options{GitHub: true, GitHubOAuthURL: gh.URL, GitHubAPIURL: gh.URL})
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := noFollow.Get(s.URL + "/auth/github/login?return_to=/device?code=ABCD")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	q := loc.Query()
	if resp.StatusCode != 302 || q.Get("code_challenge_method") != "S256" || q.Get("state") == "" || q.Get("scope") != "" {
		t.Fatalf("authorize redirect: %d %s", resp.StatusCode, loc)
	}
	var oauth *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "mrt_oauth" {
			oauth = c
		}
	}
	if oauth == nil || !oauth.HttpOnly || oauth.SameSite != http.SameSiteLaxMode {
		t.Fatalf("oauth cookie: %+v", oauth)
	}
	callback := func(state, code string) *http.Response {
		req, _ := http.NewRequest("GET", s.URL+"/auth/github/callback?state="+state+"&code="+code, nil)
		req.AddCookie(oauth)
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp
	}
	if r := callback("wrong", "good-code"); !strings.Contains(r.Header.Get("Location"), "/login?error=state") {
		t.Fatalf("bad state: %s", r.Header.Get("Location"))
	}
	r := callback(q.Get("state"), "good-code")
	if r.Header.Get("Location") != "http://localhost:3000/device?code=ABCD" {
		t.Fatalf("callback redirect: %s", r.Header.Get("Location"))
	}
	if gotVerifier == "" || gotToken != "Bearer fake-github-access" {
		t.Fatalf("PKCE verifier %q, /user auth %q", gotVerifier, gotToken)
	}
	var session *http.Cookie
	for _, c := range r.Cookies() {
		if c.Name == "mrt_session" {
			session = c
		}
	}
	if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode || session.MaxAge <= 0 {
		t.Fatalf("session cookie: %+v", session)
	}
	req, _ := http.NewRequest("GET", s.URL+"/v1/me", nil)
	req.AddCookie(session)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var me struct{ Login, DisplayName string }
	_ = json.NewDecoder(resp.Body).Decode(&me)
	_ = resp.Body.Close()
	if me.Login != "octo" {
		t.Fatalf("me after GitHub login: %+v", me)
	}
	// Open redirects are neutralized.
	resp, _ = noFollow.Get(s.URL + "/auth/github/login?return_to=//evil.example")
	_ = resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == "mrt_oauth" && strings.Contains(c.Value, "ZXZpbC") {
			t.Fatal("open redirect target stored")
		}
	}
}

func TestCORSAndDevRoutes(t *testing.T) {
	s := testserver.Start(t, testserver.Options{})
	pre := func(origin string) *http.Response {
		req, _ := http.NewRequest("OPTIONS", s.URL+"/v1/orgs", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp
	}
	if r := pre("http://localhost:3000"); r.StatusCode != 204 || r.Header.Get("Access-Control-Allow-Origin") != "http://localhost:3000" || r.Header.Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("allowed preflight: %d %v", r.StatusCode, r.Header)
	}
	if r := pre("https://evil.example"); r.StatusCode != 403 || r.Header.Get("Access-Control-Allow-Origin") != "" {
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
	if r, _ := get(t, ts.URL+"/auth/dev/login?login=alice", ""); r.StatusCode != 404 {
		t.Fatalf("dev login without flag: %d", r.StatusCode)
	}
	if r, _ := post(t, ts.URL+"/v1/dev/token", "", "application/json", []byte(`{"login":"alice"}`)); r.StatusCode != 404 && r.StatusCode != 405 {
		t.Fatalf("dev token without flag: %d", r.StatusCode)
	}
	if r, _ := get(t, ts.URL+"/auth/github/login", ""); r.StatusCode != 503 {
		t.Fatalf("github login unconfigured: %d", r.StatusCode)
	}
}
