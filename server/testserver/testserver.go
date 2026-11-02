// Package testserver runs the real sync API in-process for tests: memory
// store, in-process event hub, fast device-flow polling, and a fake Supabase
// project (an ES256 key and its JWKS endpoint) so dashboard requests carry
// real, verifiable access tokens. Dev login is on unless Options.Prod.
// The CLI's syncclient tests use it for end-to-end login and sync tests.
// Only test code may import it; it never reaches the shipped binaries.
package testserver

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/tolgayayci/muretto/server/internal/api"
	"github.com/tolgayayci/muretto/server/internal/config"
	"github.com/tolgayayci/muretto/server/internal/events"
	"github.com/tolgayayci/muretto/server/internal/store/memstore"
)

// Clock is a settable clock. Zero value starts at the real time.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

// Now returns the current fake time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance moves the clock forward.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// Options tweak the server.
type Options struct {
	IngestPerMinute    int
	MaxBody            int64
	DevicePollInterval time.Duration // default 20 ms
	Heartbeat          time.Duration // default 20 s
	RealClock          bool          // use time.Now instead of Clock
	// Prod turns dev login off: no /v1/dev/token, and CLI tokens are
	// refused where a dashboard user is required.
	Prod bool
}

// Server is a running test server.
type Server struct {
	URL   string
	Store *memstore.Store
	Hub   *events.Hub
	Clock *Clock
	HTTP  *httptest.Server
	// SupabaseURL is the fake Supabase project; tokens are issued by
	// SupabaseURL + "/auth/v1".
	SupabaseURL string

	now   func() time.Time
	key   *ecdsa.PrivateKey
	kid   string
	mu    sync.Mutex
	jwksN int
}

// Start starts a server and stops it when the test ends.
func Start(t testing.TB, o Options) *Server {
	t.Helper()
	if o.DevicePollInterval == 0 {
		o.DevicePollInterval = 20 * time.Millisecond
	}
	if o.IngestPerMinute == 0 {
		o.IngestPerMinute = 600
	}
	if o.MaxBody == 0 {
		o.MaxBody = 8 << 20
	}
	clock := &Clock{t: time.Now().UTC()}
	now := clock.Now
	if o.RealClock {
		now = time.Now
	}
	st := memstore.New()
	hub := events.NewHub()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	out := &Server{Store: st, Hub: hub, Clock: clock, now: now, key: key, kid: "test-key-1"}
	supa := httptest.NewServer(http.HandlerFunc(out.serveJWKS))
	t.Cleanup(supa.Close)
	out.SupabaseURL = supa.URL

	ts := httptest.NewUnstartedServer(nil)
	ts.Start()
	cfg := config.Config{
		Addr: ts.Listener.Addr().String(), PublicURL: ts.URL, DashboardURL: "http://localhost:3000",
		SupabaseURL: supa.URL, DevLogin: !o.Prod, MaxBody: o.MaxBody, IngestPerMinute: o.IngestPerMinute,
	}
	srv, err := api.New(api.Options{
		Config: cfg, Store: st, Hub: hub, PublishLocal: true, Now: now,
		Logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		DevicePollInterval: o.DevicePollInterval, Heartbeat: o.Heartbeat,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts.Config.Handler = srv.Handler()
	t.Cleanup(ts.Close)
	out.URL, out.HTTP = ts.URL, ts
	return out
}

func (s *Server) serveJWKS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/auth/v1/.well-known/jwks.json" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	s.jwksN++
	s.mu.Unlock()
	raw, _ := s.key.PublicKey.Bytes() // 0x04 || X || Y
	enc := base64.RawURLEncoding.EncodeToString
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
		"kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "kid": s.kid, "x": enc(raw[1:33]), "y": enc(raw[33:]),
	}}})
}

// JWKSFetches counts requests to the fake JWKS endpoint.
func (s *Server) JWKSFetches() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jwksN
}

// SupabaseSubject is the fake Supabase user id for a login.
func SupabaseSubject(login string) string {
	h := sha256.Sum256([]byte("testserver-user:" + login))
	x := hex.EncodeToString(h[:16])
	return x[:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:]
}

// SupabaseClaims are the claims of a valid access token for login, issued now.
func (s *Server) SupabaseClaims(login string) jwt.MapClaims {
	now := s.now()
	return jwt.MapClaims{
		"iss": s.SupabaseURL + "/auth/v1", "aud": "authenticated", "role": "authenticated",
		"sub": SupabaseSubject(login), "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"user_metadata": map[string]any{"user_name": login, "full_name": strings.ToUpper(login[:1]) + login[1:]},
	}
}

// SignSupabase signs claims with the fake project's key (ES256, with kid).
func (s *Server) SignSupabase(t testing.TB, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = s.kid
	raw, err := tok.SignedString(s.key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// SupabaseToken is a fresh, valid access token for login.
func (s *Server) SupabaseToken(t testing.TB, login string) string {
	t.Helper()
	return s.SignSupabase(t, s.SupabaseClaims(login))
}

// Browser is a signed-in dashboard user. Like the real dashboard's server, it
// sends a Supabase access token as a Bearer token; a fresh one per request,
// so tests that move the clock never hit an expired token.
type Browser struct {
	t     testing.TB
	s     *Server
	Login string
}

// Login signs login in through the fake Supabase project and checks that the
// API accepts the token.
func (s *Server) Login(t testing.TB, login string) *Browser {
	t.Helper()
	b := &Browser{t: t, s: s, Login: login}
	b.JSON("GET", "/v1/me", nil, 200, nil)
	return b
}

// AccessToken is a fresh Supabase access token for this user.
func (b *Browser) AccessToken() string { return b.s.SupabaseToken(b.t, b.Login) }

// Reply is a finished response: its body has been read and closed.
type Reply struct {
	StatusCode int
	Header     http.Header
}

// Do sends a request with a fresh Supabase access token.
func (b *Browser) Do(method, path string, body any) (*Reply, []byte) {
	b.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, b.s.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+b.AccessToken())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return &Reply{StatusCode: resp.StatusCode, Header: resp.Header}, out
}

// JSON sends a request, checks the status and decodes the body into out.
func (b *Browser) JSON(method, path string, body any, want int, out any) {
	b.t.Helper()
	resp, raw := b.Do(method, path, body)
	if resp.StatusCode != want {
		b.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, want, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			b.t.Fatalf("%s %s: %v", method, path, err)
		}
	}
}

// Approve approves a CLI device code as this user.
func (b *Browser) Approve(userCode string) {
	b.t.Helper()
	b.JSON("POST", "/v1/device/approve", map[string]string{"user_code": userCode}, 200, nil)
}

// Token returns a CLI token for login's dashboard user, minted through the
// real device flow (code, dashboard approval, token poll). Works with and
// without dev login.
func (s *Server) Token(t testing.TB, login string) string {
	t.Helper()
	return s.DeviceToken(t, s.Login(t, login), "test-host")
}

// DeviceToken runs the device flow for a CLI on host and approves it as b.
func (s *Server) DeviceToken(t testing.TB, b *Browser, host string) string {
	t.Helper()
	post := func(path, body string) (int, map[string]any) {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, s.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m
	}
	hostJSON, _ := json.Marshal(host)
	st, code := post("/v1/device/code", `{"client":"muretto-cli","host":`+string(hostJSON)+`}`)
	dc, _ := code["device_code"].(string)
	uc, _ := code["user_code"].(string)
	if st != 200 || dc == "" || uc == "" {
		t.Fatalf("device code: %d %v", st, code)
	}
	b.Approve(uc)
	st, tok := post("/v1/device/token", `{"device_code":"`+dc+`"}`)
	raw, _ := tok["access_token"].(string)
	if st != 200 || !strings.HasPrefix(raw, "mrt_") {
		t.Fatalf("device token: %d %v", st, tok["error"])
	}
	return raw
}

// DevToken issues a CLI token for the dev user "dev:<login>" through the
// dev-only route (not available with Options.Prod).
func (s *Server) DevToken(t testing.TB, login string) string {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, s.URL+"/v1/dev/token", strings.NewReader(`{"login":"`+login+`","name":"`+strings.ToUpper(login[:1])+login[1:]+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.AccessToken == "" {
		t.Fatalf("dev token: status %d %v", resp.StatusCode, err)
	}
	return out.AccessToken
}
