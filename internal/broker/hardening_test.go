package broker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCanonicalHost(t *testing.T) {
	ok := map[string]string{
		"api.supabase.com": "api.supabase.com",
		"API.Supabase.COM": "api.supabase.com",
		"api.example.com":  "api.example.com",
		"a-b.xn--p1ai":     "a-b.xn--p1ai",
	}
	for in, want := range ok {
		got, err := CanonicalHost(in)
		if err != nil || got != want {
			t.Errorf("CanonicalHost(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "api.supabase.com:443", "api.supabase.com:", "api.supabase.com.",
		"u@api.supabase.com", "127.0.0.1", "127.1", "0x7f.0x1", "[::1]", "::1",
		"localhost", "api..supabase.com", ".api.supabase.com", "api.supabase.com%2e",
		"-api.supabase.com", "api_x.supabase.com", "api.supabase.123",
	} {
		if got, err := CanonicalHost(in); !errors.Is(err, ErrRefused) {
			t.Errorf("CanonicalHost(%q) = %q, %v; want refusal", in, got, err)
		}
	}
}

// Reviewer finding 1: equivalent host spellings, case and query values must
// not get past the deny list or the decrypt rule, whatever the rules say.
func TestDenyListBypassesRefused(t *testing.T) {
	rules := []Rule{
		{Host: "api.supabase.com:443", Method: "GET", PathPattern: "/v1/projects/{ref}/secrets"},
		{Host: "api.supabase.com.", Method: "GET", PathPattern: "/v1/branches/{id}"},
		{Host: "API.SUPABASE.COM", Method: "GET", PathPattern: "/v1/projects/{ref}/{x}"},
		{Host: "api.supabase.com", Method: "GET", PathPattern: "/{a}/{b}/{c}/{d}"},
		{Host: "api.supabase.com", Method: "GET", PathPattern: "/{a}/{b}/{c}"},
		{Host: "api.vercel.com", Method: "GET", PathPattern: "/v10/projects/{id}/env", Query: []string{"teamId"}},
		{Host: "api.vercel.com", Method: "GET", PathPattern: "/{v}/projects/{id}/env/{e}"},
	}
	b, ft := newTestBroker(t, rules)
	for _, u := range []string{
		"https://api.supabase.com:443/v1/projects/abc/secrets",
		"https://api.supabase.com./v1/branches/abc",
		"https://api.supabase.com/v1/branches/abc",
		"https://API.SUPABASE.COM/v1/projects/abc/Secrets",
		"https://api.supabase.com/v1/projects/abc/SECRETS",
		"https://api.supabase.com/V1/Projects/abc/API-KEYS",
		"https://api.supabase.com/V1/BRANCHES/abc",
		"https://api.supabase.com/v1/Branches/abc",
		"https://api.supabase.com/v1/projects/abc/Config/Auth",
		"https://api.vercel.com/V9/projects/p/ENV/abc",
		"https://api.vercel.com/v10/projects/p/env?teamId=decrypt",
		"https://api.vercel.com/v10/projects/p/env?teamId=x-DeCrYpT-y",
		"https://api.vercel.com/v10/projects/p/env?teamId=%64ecrypt",
		"https://api.vercel.com/v10/projects/p/env?teamId=t&teamId=decrypt",
		"https://api.vercel.com/v10/projects/p/env?%64ecrypt=1",
		"https://127.0.0.1/v1/projects/abc/functions",
		"https://[::1]/v1/projects/abc/functions",
		"https://api.supabase.com/v1/projects/abc%2Fsecrets",
		"https://api.supabase.com/v1/projects/abc%2fx/secrets",
		"https://api.supabase.com/v1/projects/%2e%2e/functions",
		"https://api.supabase.com/v1/projects/./functions",
		"https://api.supabase.com/v1//projects/abc",
	} {
		req, err := NewRequest(context.Background(), "GET", u, nil)
		if err != nil {
			t.Fatalf("%s: %v", u, err)
		}
		if _, err := b.Check(req); !errors.Is(err, ErrRefused) {
			t.Errorf("Check(%s) = %v, want refusal", u, err)
		}
		if _, err := doReq(b, req); !errors.Is(err, ErrRefused) {
			t.Errorf("Do(%s) = %v, want refusal", u, err)
		}
	}
	if n := ft.calls.Load(); n != 0 {
		t.Fatalf("refused request reached the transport (%d calls)", n)
	}
}

// A rule with a non-canonical host never matches anything, so a bad rule fails
// closed. An upper-case rule host is just lowered.
func TestRuleHostsCanonicalised(t *testing.T) {
	b, ft := newTestBroker(t, []Rule{
		{Host: "api.supabase.com:443", Method: "GET", PathPattern: "/v1/projects"},
		{Host: "api.supabase.com.", Method: "GET", PathPattern: "/v1/organizations"},
		{Host: "API.VERCEL.COM", Method: "GET", PathPattern: "/v2/teams"},
	})
	for _, u := range []string{
		"https://api.supabase.com/v1/projects",
		"https://api.supabase.com/v1/organizations",
	} {
		if _, err := do(t, b, "GET", u, nil); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: want refusal, got %v", u, err)
		}
	}
	if _, err := do(t, b, "GET", "https://api.vercel.com/v2/teams", nil); err != nil {
		t.Fatalf("lower-cased rule host should match: %v", err)
	}
	if ft.calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", ft.calls.Load())
	}
	for _, r := range b.Rules() {
		if r.Host != "" && r.Host != strings.ToLower(r.Host) {
			t.Fatalf("rule host not canonical: %q", r.Host)
		}
	}
}

// streamBody is a body whose length is unknown up front, so ContentLength is 0.
type streamBody struct{ io.Reader }

func (streamBody) Close() error { return nil }

// Reviewer finding 3: a GET with a streaming body (ContentLength 0) was sent.
func TestGETWithAnyBodyRefused(t *testing.T) {
	b, ft := newTestBroker(t, testRules())
	bodies := map[string]func(*http.Request){
		"stream, length 0": func(r *http.Request) {
			r.Body = streamBody{strings.NewReader(`{"query":"drop table users"}`)}
			r.ContentLength = 0
		},
		"empty stream": func(r *http.Request) {
			r.Body = streamBody{strings.NewReader("")}
			r.ContentLength = 0
		},
		"length without body": func(r *http.Request) { r.ContentLength = 5 },
		"chunked":             func(r *http.Request) { r.TransferEncoding = []string{"chunked"} },
		"trailer":             func(r *http.Request) { r.Trailer = http.Header{"X": nil} },
		"method override":     func(r *http.Request) { r.Header.Set("X-HTTP-Method-Override", "DELETE") },
		"method override 2":   func(r *http.Request) { r.Header.Set("X-Method-Override", "DELETE") },
	}
	for name, set := range bodies {
		for _, method := range []string{"GET", ""} {
			req, err := NewRequest(context.Background(), "GET", "https://api.supabase.com/v1/projects", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Method = method
			set(req)
			if _, err := doReq(b, req); !errors.Is(err, ErrRefused) {
				t.Errorf("%s (method %q): want refusal, got %v", name, method, err)
			}
		}
	}
	if ft.calls.Load() != 0 {
		t.Fatalf("GET with a body reached the transport")
	}
	// NoBody is fine.
	req, _ := NewRequest(context.Background(), "GET", "https://api.supabase.com/v1/projects", nil)
	req.Body = http.NoBody
	if _, err := doReq(b, req); err != nil {
		t.Fatalf("GET with NoBody refused: %v", err)
	}
}

// shiftyBody returns the pinned body on the first read pass and other bytes on
// any later one, the way a malicious streaming reader could.
type shiftyBody struct {
	first, later []byte
	r            io.Reader
	opened       int
}

func (s *shiftyBody) Read(p []byte) (int, error) {
	if s.r == nil {
		s.opened++
		if s.opened == 1 {
			s.r = bytes.NewReader(s.first)
		} else {
			s.r = bytes.NewReader(s.later)
		}
	}
	return s.r.Read(p)
}

func (s *shiftyBody) Close() error { return nil }

// Reviewer finding 3: the POST body is read once, hashed, and exactly those
// bytes are sent, whatever the caller's reader, GetBody or ContentLength do.
func TestPOSTSendsExactlyHashedBytes(t *testing.T) {
	pinned, _, _ := PinnedBody("supabase_rls_catalog_v1")
	evil := []byte(`{"query":"drop table users"}`)
	b, ft := newTestBroker(t, testRules())
	var got []byte
	var gotLen int64
	ft.reply = func(req *http.Request) *http.Response {
		got, _ = io.ReadAll(req.Body)
		gotLen = req.ContentLength
		if req.GetBody != nil {
			rb, _ := req.GetBody()
			again, _ := io.ReadAll(rb)
			if !bytes.Equal(again, pinned) {
				t.Errorf("GetBody returns other bytes: %q", again)
			}
		}
		return jsonResp(201, `[]`)
	}
	req, _ := NewRequest(context.Background(), "POST", "https://api.supabase.com/v1/projects/abc/database/query/read-only", nil)
	req.Body = &shiftyBody{first: pinned, later: evil}
	req.ContentLength = 0
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(evil)), nil }
	if _, err := doReq(b, req); err != nil {
		t.Fatalf("pinned POST refused: %v", err)
	}
	if !bytes.Equal(got, pinned) || gotLen != int64(len(pinned)) {
		t.Fatalf("sent %q (len %d), want the pinned body", got, gotLen)
	}
}

func TestPOSTBodyCapped(t *testing.T) {
	old := MaxRequestBodyBytes
	MaxRequestBodyBytes = 16
	defer func() { MaxRequestBodyBytes = old }()
	pinned, _, _ := PinnedBody("supabase_rls_catalog_v1")
	b, ft := newTestBroker(t, testRules())
	_, err := do(t, b, "POST", "https://api.supabase.com/v1/projects/abc/database/query/read-only", pinned)
	if !errors.Is(err, ErrRefused) || ft.calls.Load() != 0 {
		t.Fatalf("oversized POST body: want refusal before I/O, got %v", err)
	}
}
