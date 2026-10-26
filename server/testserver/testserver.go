// Package testserver runs the real sync API in-process for tests: memory
// store, in-process event hub, dev login enabled, fast device-flow polling.
// The CLI's syncclient tests use it for end-to-end login and sync tests.
// Only test code may import it; it never reaches the shipped binaries.
package testserver

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

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
	GitHubOAuthURL     string
	GitHubAPIURL       string
	GitHub             bool // configure fake OAuth client credentials
	RealClock          bool // use time.Now instead of Clock
}

// Server is a running test server.
type Server struct {
	URL   string
	Store *memstore.Store
	Hub   *events.Hub
	Clock *Clock
	HTTP  *httptest.Server
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
	ts := httptest.NewUnstartedServer(nil)
	ts.Start()
	cfg := config.Config{
		Addr: ts.Listener.Addr().String(), PublicURL: ts.URL, DashboardURL: "http://localhost:3000",
		DevLogin: true, MaxBody: o.MaxBody, IngestPerMinute: o.IngestPerMinute,
	}
	if o.GitHub {
		cfg.GitHubClientID, cfg.GitHubClientSecret = "test-client-id", "test-client-value"
	}
	srv, err := api.New(api.Options{
		Config: cfg, Store: st, Hub: hub, PublishLocal: true, Now: now,
		Logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		DevicePollInterval: o.DevicePollInterval, Heartbeat: o.Heartbeat,
		GitHubOAuthURL: o.GitHubOAuthURL, GitHubAPIURL: o.GitHubAPIURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts.Config.Handler = srv.Handler()
	t.Cleanup(ts.Close)
	return &Server{URL: ts.URL, Store: st, Hub: hub, Clock: clock, HTTP: ts}
}

// Browser is a dashboard user: a session cookie plus its CSRF token.
type Browser struct {
	t      testing.TB
	s      *Server
	Cookie string
	CSRF   string
	Login  string
}

// Login signs in through the dev-only login route.
func (s *Server) Login(t testing.TB, login string) *Browser {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(s.URL + "/auth/dev/login?login=" + url.QueryEscape(login) + "&name=" + url.QueryEscape(strings.ToUpper(login[:1])+login[1:]))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	b := &Browser{t: t, s: s, Login: login}
	for _, c := range resp.Cookies() {
		if c.Name == "mrt_session" {
			b.Cookie = c.Value
		}
	}
	if b.Cookie == "" {
		t.Fatalf("dev login for %s set no session cookie (status %d)", login, resp.StatusCode)
	}
	var me struct {
		CSRF string `json:"csrf_token"`
	}
	b.JSON("GET", "/v1/me", nil, 200, &me)
	b.CSRF = me.CSRF
	return b
}

// Do sends a request with the session cookie and CSRF header.
func (b *Browser) Do(method, path string, body any) (*http.Response, []byte) {
	b.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, b.s.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: "mrt_session", Value: b.Cookie})
	if b.CSRF != "" {
		req.Header.Set("X-CSRF-Token", b.CSRF)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp, out
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

// Token issues a CLI token through the dev-only token route.
func (s *Server) Token(t testing.TB, login string) string {
	t.Helper()
	resp, err := http.Post(s.URL+"/v1/dev/token", "application/json", strings.NewReader(`{"login":"`+login+`","name":"`+strings.ToUpper(login[:1])+login[1:]+`"}`))
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
