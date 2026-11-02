package broker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTransport answers every request it receives and counts them. A refused
// request must never reach it.
type fakeTransport struct {
	calls atomic.Int32
	reply func(req *http.Request) *http.Response
	block bool // wait for the request context and fail
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.calls.Add(1)
	if f.block {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	if f.reply != nil {
		return f.reply(req), nil
	}
	return jsonResp(200, `{"ok":true}`), nil
}

func jsonResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func testRules() []Rule {
	_, sha, _ := PinnedBody("supabase_rls_catalog_v1")
	return []Rule{
		{Provider: "supabase", Host: "api.supabase.com", Method: "GET", PathPattern: "/v1/projects"},
		{Provider: "supabase", Host: "api.supabase.com", Method: "GET", PathPattern: "/v1/projects/{ref}/functions"},
		{Provider: "supabase", Host: "api.supabase.com", Method: "GET", PathPattern: "/v1/projects/{ref}/advisors/security", Query: []string{"lint_type"}},
		{Provider: "supabase", Host: "api.supabase.com", Method: "POST", PathPattern: "/v1/projects/{ref}/database/query/read-only", BodySHA256: sha},
		{Provider: "vercel", Host: "api.vercel.com", Method: "GET", PathPattern: "/v10/projects/{idOrName}/env", Query: []string{"teamId"}, Strip: []string{"envs[].comment"}},
		{Provider: "vercel", Host: "api.vercel.com", Method: "GET", PathPattern: "/v10/projects", Query: []string{"teamId", "limit", "until"}, Strip: []string{"projects[].env", "projects[].link.deployHooks"}},
	}
}

func newTestBroker(t *testing.T, rules []Rule) (*Broker, *fakeTransport) {
	t.Helper()
	ft := &fakeTransport{}
	return New(&http.Client{Transport: ft}, rules), ft
}

func do(t *testing.T, b *Broker, method, url string, body []byte) (*http.Response, error) {
	t.Helper()
	req, err := NewRequest(context.Background(), method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := b.Do(context.Background(), req)
	if err == nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	return resp, err
}

func TestAllowedGET(t *testing.T) {
	b, ft := newTestBroker(t, testRules())
	for _, u := range []string{
		"https://api.supabase.com/v1/projects",
		"https://api.supabase.com/v1/projects/abcdefghijklmnopqrst/functions",
		"https://api.supabase.com/v1/projects/abcdefghijklmnopqrst/advisors/security?lint_type=sql",
		"https://api.vercel.com/v10/projects/prj_EXAMPLE/env?teamId=team_EXAMPLE",
		"https://api.vercel.com/v10/projects?teamId=team_EXAMPLE&limit=100&until=1793000000000",
	} {
		if _, err := do(t, b, "GET", u, nil); err != nil {
			t.Errorf("%s: unexpected refusal: %v", u, err)
		}
	}
	if got := ft.calls.Load(); got != 5 {
		t.Fatalf("transport calls = %d, want 5", got)
	}
}

func TestRefusedBeforeIO(t *testing.T) {
	catalog, _, _ := PinnedBody("supabase_rls_catalog_v1")
	cases := []struct {
		name, method, url string
		body              []byte
	}{
		{"unknown path", "GET", "https://api.supabase.com/v1/organizations", nil},
		{"unknown host", "GET", "https://evil.example.com/v1/projects", nil},
		{"host suffix trick", "GET", "https://api.supabase.com.evil.example.com/v1/projects", nil},
		{"http scheme", "GET", "http://api.supabase.com/v1/projects", nil},
		{"credentials in URL", "GET", "https://" + "user" + ":" + "pw" + "@api.supabase.com/v1/projects", nil},
		{"extra segment", "GET", "https://api.supabase.com/v1/projects/abc/functions/x", nil},
		{"dot-dot segment", "GET", "https://api.supabase.com/v1/projects/../functions", nil},
		{"trailing slash on a rule without one", "GET", "https://api.supabase.com/v1/projects/", nil},
		{"double trailing slash", "GET", "https://api.supabase.com/v1/projects//", nil},
		{"empty middle segment", "GET", "https://api.supabase.com/v1//projects", nil},
		{"root slash only", "GET", "https://api.supabase.com/", nil},
		{"denied path with a trailing slash", "GET", "https://api.vercel.com/v9/projects/prj_1/env/env_1/", nil},
		{"encoded slash", "GET", "https://api.supabase.com/v1/projects/a%2Fb/functions", nil},
		{"query not allowed", "GET", "https://api.supabase.com/v1/projects?foo=1", nil},
		{"query on wrong endpoint", "GET", "https://api.supabase.com/v1/projects/abc/functions?lint_type=sql", nil},
		{"vercel decrypt true", "GET", "https://api.vercel.com/v10/projects/prj_EXAMPLE/env?teamId=t&decrypt=true", nil},
		{"vercel decrypt false", "GET", "https://api.vercel.com/v10/projects/prj_EXAMPLE/env?decrypt=false", nil},
		{"vercel decrypt case", "GET", "https://api.vercel.com/v10/projects/prj_EXAMPLE/env?Decrypt=1", nil},
		{"vercel single env value", "GET", "https://api.vercel.com/v1/projects/prj_EXAMPLE/env/abc", nil},
		{"vercel decrypt in path", "GET", "https://api.vercel.com/v1/decrypt", nil},
		{"supabase branch detail", "GET", "https://api.supabase.com/v1/branches/tsrqponmlkjihgfedcba", nil},
		{"supabase function body", "GET", "https://api.supabase.com/v1/projects/abc/functions/x/body", nil},
		{"DELETE", "DELETE", "https://api.supabase.com/v1/projects", nil},
		{"PUT", "PUT", "https://api.supabase.com/v1/projects", []byte(`{}`)},
		{"PATCH", "PATCH", "https://api.vercel.com/v10/projects", []byte(`{}`)},
		{"HEAD", "HEAD", "https://api.supabase.com/v1/projects", nil},
		{"POST on a GET rule", "POST", "https://api.supabase.com/v1/projects", []byte(`{}`)},
		{"POST other SQL", "POST", "https://api.supabase.com/v1/projects/abc/database/query/read-only", []byte(`{"query":"delete from customers"}`)},
		{"POST pinned SQL plus space", "POST", "https://api.supabase.com/v1/projects/abc/database/query/read-only", append(append([]byte(nil), catalog...), ' ')},
		{"POST pinned SQL to write endpoint", "POST", "https://api.supabase.com/v1/projects/abc/database/query", catalog},
		{"POST without body", "POST", "https://api.supabase.com/v1/projects/abc/database/query/read-only", nil},
		{"GET with body", "GET", "https://api.supabase.com/v1/projects", []byte(`x`)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, ft := newTestBroker(t, testRules())
			_, err := do(t, b, c.method, c.url, c.body)
			if !errors.Is(err, ErrRefused) {
				t.Fatalf("want ErrRefused, got %v", err)
			}
			if n := ft.calls.Load(); n != 0 {
				t.Fatalf("refused request reached the transport (%d calls)", n)
			}
		})
	}
}

func TestPinnedPOSTAllowed(t *testing.T) {
	body, _, ok := PinnedBody("supabase_rls_catalog_v1")
	if !ok {
		t.Fatal("pinned body missing")
	}
	b, ft := newTestBroker(t, testRules())
	var got []byte
	ft.reply = func(req *http.Request) *http.Response {
		got, _ = io.ReadAll(req.Body)
		return jsonResp(201, `[]`)
	}
	if _, err := do(t, b, "POST", "https://api.supabase.com/v1/projects/abcdefghijklmnopqrst/database/query/read-only", body); err != nil {
		t.Fatalf("pinned POST refused: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body changed in transit")
	}
}

// A POST rule whose hash is not declared in Go (e.g. one written in YAML) is
// never honoured, even if the request body matches it.
func TestPOSTHashMustBeDeclaredInGo(t *testing.T) {
	body := []byte(`{"query":"select 1"}`)
	sum := sha256Hex(body)
	b, ft := newTestBroker(t, []Rule{{Host: "api.supabase.com", Method: "POST", PathPattern: "/v1/x", BodySHA256: sum}})
	_, err := do(t, b, "POST", "https://api.supabase.com/v1/x", body)
	if !errors.Is(err, ErrRefused) || ft.calls.Load() != 0 {
		t.Fatalf("undeclared POST hash must be refused before I/O, got %v", err)
	}
}

func TestDeniedEvenIfAllowlisted(t *testing.T) {
	rules := []Rule{
		{Host: "api.supabase.com", Method: "GET", PathPattern: "/v1/branches/{id}"},
		{Host: "api.supabase.com", Method: "GET", PathPattern: "/v1/projects/{ref}/secrets"},
		{Host: "api.vercel.com", Method: "GET", PathPattern: "/v1/projects/{p}/env/{id}"},
	}
	b, ft := newTestBroker(t, rules)
	for _, u := range []string{
		"https://api.supabase.com/v1/branches/tsrqponmlkjihgfedcba",
		"https://api.supabase.com/v1/projects/abc/secrets",
		"https://api.vercel.com/v1/projects/prj_EXAMPLE/env/abc",
	} {
		if _, err := do(t, b, "GET", u, nil); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: want refusal, got %v", u, err)
		}
	}
	if ft.calls.Load() != 0 {
		t.Fatal("denied endpoint reached the transport")
	}
}

func TestRedirects(t *testing.T) {
	cases := []struct {
		name, location string
		allowed        bool
	}{
		{"other host", "https://evil.example.com/v1/projects", false},
		{"same host, not allowlisted", "https://api.supabase.com/v1/organizations", false},
		{"same host, http", "http://api.supabase.com/v1/projects", false},
		{"same host, allowlisted", "https://api.supabase.com/v1/projects", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, ft := newTestBroker(t, testRules())
			ft.reply = func(req *http.Request) *http.Response {
				if req.URL.Path == "/v1/projects/abc/functions" {
					r := jsonResp(302, "")
					r.Header.Set("Location", c.location)
					return r
				}
				return jsonResp(200, `[]`)
			}
			_, err := do(t, b, "GET", "https://api.supabase.com/v1/projects/abc/functions", nil)
			if c.allowed {
				if err != nil {
					t.Fatalf("unexpected error %v", err)
				}
				if ft.calls.Load() != 2 {
					t.Fatalf("calls = %d", ft.calls.Load())
				}
				return
			}
			if !errors.Is(err, ErrRefused) {
				t.Fatalf("want refusal, got %v", err)
			}
			if ft.calls.Load() != 1 {
				t.Fatalf("redirect target was requested (%d calls)", ft.calls.Load())
			}
		})
	}
}

func TestResponseSizeCap(t *testing.T) {
	old := MaxResponseBytes
	MaxResponseBytes = 1024
	defer func() { MaxResponseBytes = old }()
	b, ft := newTestBroker(t, testRules())
	ft.reply = func(*http.Request) *http.Response {
		return jsonResp(200, `["`+strings.Repeat("a", 2000)+`"]`)
	}
	_, err := do(t, b, "GET", "https://api.supabase.com/v1/projects", nil)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestTimeout(t *testing.T) {
	b, ft := newTestBroker(t, testRules())
	if b.client.Timeout != DefaultTimeout {
		t.Fatalf("default timeout not applied: %v", b.client.Timeout)
	}
	b.client.Timeout = 50 * time.Millisecond
	ft.block = true
	start := time.Now()
	_, err := do(t, b, "GET", "https://api.supabase.com/v1/projects", nil)
	if err == nil {
		t.Fatal("want timeout error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("timeout not enforced")
	}
}

func TestCallerClientUnchanged(t *testing.T) {
	c := &http.Client{Transport: &fakeTransport{}}
	New(c, nil)
	if c.CheckRedirect != nil || c.Timeout != 0 {
		t.Fatal("New modified the caller's client")
	}
}

func TestStripping(t *testing.T) {
	b, ft := newTestBroker(t, testRules())
	ft.reply = func(req *http.Request) *http.Response {
		if strings.HasSuffix(req.URL.Path, "/env") {
			return jsonResp(200, `{"envs":[{"key":"A","value":"MURETTO_FAKE_SECRET_1","vsmValue":"MURETTO_FAKE_SECRET_2","legacyValue":"MURETTO_FAKE_SECRET_3","comment":"MURETTO_FAKE_SECRET_4","createdAt":1793000100000}]}`)
		}
		return jsonResp(200, `{"projects":[{"id":"prj_EXAMPLE","env":[{"key":"A","value":"MURETTO_FAKE_SECRET_5"}],"protectionBypass":{"MURETTO_FAKE_SECRET_6":{"scope":"automation-bypass"}},"link":{"type":"github","deployHooks":[{"url":"MURETTO_FAKE_SECRET_7"}]}}]}`)
	}
	for _, u := range []string{
		"https://api.vercel.com/v10/projects/prj_EXAMPLE/env?teamId=team_EXAMPLE",
		"https://api.vercel.com/v10/projects?teamId=team_EXAMPLE",
	} {
		resp, err := do(t, b, "GET", u, nil)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		if bytes.Contains(body, []byte("MURETTO_FAKE_SECRET")) {
			t.Fatalf("secret survived stripping: %s", body)
		}
		var v any
		if err := json.Unmarshal(body, &v); err != nil {
			t.Fatalf("sanitized body is not JSON: %v", err)
		}
		if strings.Contains(u, "/env") && !bytes.Contains(body, []byte("1793000100000")) {
			t.Fatalf("large numbers must survive re-encoding: %s", body)
		}
	}
}

func TestPinnedQueryShape(t *testing.T) {
	q := strings.ToLower(SupabaseRLSCatalogQuery)
	if strings.Contains(q, ";") || strings.Contains(q, " set ") || !strings.HasPrefix(q, "select ") {
		t.Fatal("pinned catalog query must be one constant SELECT without ';' or SET")
	}
	for _, w := range []string{"insert", "update ", "delete", "drop", "alter", "grant", "create", "truncate"} {
		if strings.Contains(q, w) {
			t.Fatalf("pinned query contains %q", w)
		}
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// A trailing slash is allowed only where a rule has it, and never dodges
// the deny list.
func TestTrailingSlash(t *testing.T) {
	ft := &fakeTransport{}
	b := New(&http.Client{Transport: ft}, []Rule{
		{Provider: "sentry", Host: "sentry.io", Method: "GET", PathPattern: "/api/0/organizations/"},
		{Provider: "vercel", Host: "api.vercel.com", Method: "GET", PathPattern: "/v9/projects/{id}/env/{env}/"},
	})
	if _, err := do(t, b, "GET", "https://sentry.io/api/0/organizations/", nil); err != nil {
		t.Errorf("rule with a trailing slash refused: %v", err)
	}
	for _, u := range []string{
		"https://sentry.io/api/0/organizations",               // rule has the slash, request does not
		"https://sentry.io/api/0/organizations//",             // two slashes
		"https://api.vercel.com/v9/projects/prj_1/env/env_1/", // denied in code even with a rule
	} {
		if _, err := do(t, b, "GET", u, nil); err == nil {
			t.Errorf("%s: not refused", u)
		}
	}
	if got := ft.calls.Load(); got != 1 {
		t.Errorf("transport calls = %d, want 1", got)
	}
}

// An offline broker answers allowed requests locally and still refuses
// everything else, strips secrets and enforces the deny list.
func TestOffline(t *testing.T) {
	var seen []string
	b := NewOffline([]Rule{
		{Provider: "acme", Host: "api.acme.dev", Method: "GET", PathPattern: "/v1/things", Query: []string{"limit"}, Strip: []string{"[].secret"}},
		{Provider: "vercel", Host: "api.vercel.com", Method: "GET", PathPattern: "/v9/projects/{id}/env/{env}"},
	}, func(method, path, rawQuery string) (int, []byte) {
		seen = append(seen, method+" "+path+"?"+rawQuery)
		return 200, []byte(`[{"id":"t1","secret":"MURETTO_FAKE_SECRET","value":"x"}]`)
	})
	resp, err := do(t, b, "GET", "https://api.acme.dev/v1/things?limit=10", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(got), "MURETTO_FAKE_SECRET") || strings.Contains(string(got), `"value"`) || !strings.Contains(string(got), "t1") {
		t.Errorf("offline response not stripped: %s", got)
	}
	for _, u := range []string{
		"https://api.acme.dev/v1/other",
		"https://api.acme.dev/v1/things?decrypt=true",
		"https://api.vercel.com/v9/projects/p/env/e",
		"https://evil.example.com/v1/things",
	} {
		if _, err := do(t, b, "GET", u, nil); err == nil {
			t.Errorf("%s: not refused offline", u)
		}
	}
	if len(seen) != 1 || seen[0] != "GET /v1/things?limit=10" {
		t.Errorf("responder saw %v", seen)
	}
}
