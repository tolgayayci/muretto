// Package broker is the only provider network client in Muretto. It refuses,
// in code and before any network I/O, every request that is not an
// allowlisted read.
//
// What Do refuses before sending anything:
//   - a URL that is not https, carries user info, or has no matching Rule
//     (provider host + method + path pattern + allowed query names);
//   - every method other than GET, except a POST whose body sha256 equals
//     both the Rule's BodySHA256 and a hash declared in Go in pinned.go;
//   - anything that mentions Vercel's "decrypt" (query name or path), and the
//     endpoints in deniedPaths, even if a Rule would allow them.
//
// What Do enforces while talking to the network:
//   - redirects are refused unless they stay on the same host and the new
//     request matches a Rule (POST redirects are always refused);
//   - a client timeout (DefaultTimeout unless the caller set one);
//   - a response size cap (MaxResponseBytes); larger bodies are an error;
//   - JSON responses lose every key in alwaysStrip at any depth, and the
//     paths listed in the matching Rule's Strip, before the caller sees them.
package broker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Rule allows one read endpoint.
type Rule struct {
	Provider    string
	Host        string   // e.g. api.supabase.com
	Method      string   // GET, or POST only with BodySHA256 set in Go code
	PathPattern string   // e.g. /v1/projects/{ref}/functions; {x} matches one path segment
	Query       []string // allowed query parameter names
	BodySHA256  string   // required for POST: hex sha256 of the only allowed body
	// Strip lists JSON paths removed from the response before Do returns it,
	// e.g. "projects[].env" or "envs[].value". "[]" walks every array element.
	// The keys in alwaysStrip are removed on top of these, at any depth.
	Strip []string
}

// Limits. Variables (not constants) only so tests can lower them.
var (
	// MaxResponseBytes caps every response body. Larger responses fail.
	MaxResponseBytes int64 = 16 << 20
	// MaxRequestBodyBytes caps a POST body before it is hashed.
	MaxRequestBodyBytes int64 = 64 << 10
	// DefaultTimeout is used when the caller's client has no timeout.
	DefaultTimeout = 30 * time.Second
)

// ErrRefused wraps every refusal made by the broker. A refused request never
// reached the network.
var ErrRefused = errors.New("broker: request refused")

// ErrTooLarge is returned when a response exceeds MaxResponseBytes.
var ErrTooLarge = errors.New("broker: response too large")

// Broker sends allowlisted requests.
type Broker struct {
	client  *http.Client
	allow   []Rule
	observe func(req *http.Request, status int, body []byte)
}

// New returns a broker that allows only the given rules. client may be nil.
// The broker works on a copy of client: it installs its own redirect policy and
// a timeout, and never changes the caller's client.
func New(client *http.Client, allow []Rule) *Broker {
	var c http.Client
	if client != nil {
		c = *client
	}
	if c.Timeout == 0 {
		c.Timeout = DefaultTimeout
	}
	b := &Broker{allow: append([]Rule(nil), allow...)}
	c.CheckRedirect = b.checkRedirect
	b.client = &c
	return b
}

// Rules returns a copy of the broker's allowlist.
func (b *Broker) Rules() []Rule { return append([]Rule(nil), b.allow...) }

// Observe registers a function that sees every sanitized response body. It is
// used by the opt-in fixture recorder; it never sees refused requests.
func (b *Broker) Observe(f func(req *http.Request, status int, body []byte)) { b.observe = f }

// NewRequest builds a request for Do. Adapters use it instead of net/http so
// that the broker stays the only package that constructs provider requests.
func NewRequest(ctx context.Context, method, rawURL string, body []byte) (*http.Request, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	return http.NewRequestWithContext(ctx, method, rawURL, r)
}

// Check reports whether req would be allowed, without sending it. It returns
// the matching rule. It may read and restore req.Body.
func (b *Broker) Check(req *http.Request) (Rule, error) {
	return b.match(req)
}

// Do sends req if it matches an allowlist rule, and fails otherwise.
func (b *Broker) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", ErrRefused)
	}
	if req == nil {
		return nil, fmt.Errorf("%w: nil request", ErrRefused)
	}
	rule, err := b.match(req)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := readCapped(resp.Body, MaxResponseBytes)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	body = sanitizeBody(body, resp.Header.Get("Content-Type"), rule.Strip)
	if b.observe != nil {
		b.observe(req, resp.StatusCode, body)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Del("Content-Length")
	return resp, nil
}

func readCapped(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: more than %d bytes", ErrTooLarge, limit)
	}
	return data, nil
}

// checkRedirect allows a redirect only to the same host and only if the new
// request matches the allowlist. It never follows a redirect of a POST.
func (b *Broker) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return fmt.Errorf("%w: too many redirects", ErrRefused)
	}
	prev := via[len(via)-1]
	if !strings.EqualFold(req.URL.Host, prev.URL.Host) {
		return fmt.Errorf("%w: redirect to another host (%s)", ErrRefused, req.URL.Host)
	}
	if prev.Method != http.MethodGet || req.Method != http.MethodGet {
		return fmt.Errorf("%w: redirect of a non-GET request", ErrRefused)
	}
	if _, err := b.match(req); err != nil {
		return err
	}
	return nil
}

var safeSegment = regexp.MustCompile(`^[A-Za-z0-9._~@-]+$`)

func refuse(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrRefused}, args...)...)
}

// match applies every refusal rule and returns the matching Rule.
func (b *Broker) match(req *http.Request) (Rule, error) {
	u := req.URL
	if u == nil {
		return Rule{}, refuse("no URL")
	}
	if u.Scheme != "https" {
		return Rule{}, refuse("scheme %q is not https", u.Scheme)
	}
	if u.User != nil {
		return Rule{}, refuse("URL carries credentials")
	}
	if u.Opaque != "" || u.Fragment != "" {
		return Rule{}, refuse("unsupported URL form")
	}
	if req.Host != "" && !strings.EqualFold(req.Host, u.Host) {
		return Rule{}, refuse("Host header differs from URL host")
	}
	host := strings.ToLower(u.Host)
	path := u.EscapedPath()
	if strings.Contains(strings.ToLower(path), "decrypt") {
		return Rule{}, refuse("decrypt is never allowed")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Rule{}, refuse("malformed query")
	}
	for name := range query {
		if strings.Contains(strings.ToLower(name), "decrypt") {
			return Rule{}, refuse("decrypt is never allowed")
		}
	}
	segs, ok := splitPath(path)
	if !ok {
		return Rule{}, refuse("unsafe path %q", path)
	}
	if d := denied(host, segs); d != "" {
		return Rule{}, refuse("endpoint %s is denied in code", d)
	}

	var bodyHash string
	switch req.Method {
	case http.MethodGet, "":
		if req.Body != nil && req.Body != http.NoBody && req.ContentLength != 0 {
			return Rule{}, refuse("GET with a body")
		}
	case http.MethodPost:
		h, err := hashBody(req)
		if err != nil {
			return Rule{}, err
		}
		bodyHash = h
	default:
		return Rule{}, refuse("method %s is never allowed", req.Method)
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}

	var lastWhy string
	for _, r := range b.allow {
		if !strings.EqualFold(r.Host, host) || !strings.EqualFold(r.Method, method) {
			continue
		}
		if !pathMatches(r.PathPattern, segs) {
			continue
		}
		if bad := disallowedQuery(r.Query, query); bad != "" {
			lastWhy = fmt.Sprintf("query parameter %q not allowed for %s %s", bad, method, r.PathPattern)
			continue
		}
		if method == http.MethodPost {
			if r.BodySHA256 == "" || r.BodySHA256 != bodyHash {
				lastWhy = "POST body does not match the pinned hash"
				continue
			}
			if _, pinned := pinnedByHash[bodyHash]; !pinned {
				lastWhy = "POST body hash is not declared in Go code"
				continue
			}
		}
		return r, nil
	}
	if lastWhy != "" {
		return Rule{}, refuse("%s", lastWhy)
	}
	return Rule{}, refuse("%s %s%s matches no allowlist rule", method, host, path)
}

func splitPath(p string) ([]string, bool) {
	if !strings.HasPrefix(p, "/") {
		return nil, false
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	for _, s := range parts {
		if s == "" || s == "." || s == ".." || !safeSegment.MatchString(s) {
			return nil, false
		}
	}
	return parts, true
}

func pathMatches(pattern string, segs []string) bool {
	pp := strings.Split(strings.TrimPrefix(pattern, "/"), "/")
	if len(pp) != len(segs) {
		return false
	}
	for i, p := range pp {
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			continue // segs are already checked by safeSegment
		}
		if p != segs[i] {
			return false
		}
	}
	return true
}

func disallowedQuery(allowed []string, q url.Values) string {
	for name := range q {
		ok := false
		for _, a := range allowed {
			if a == name {
				ok = true
				break
			}
		}
		if !ok {
			return name
		}
	}
	return ""
}

func hashBody(req *http.Request) (string, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return "", refuse("POST without a body")
	}
	data, err := io.ReadAll(io.LimitReader(req.Body, MaxRequestBodyBytes+1))
	_ = req.Body.Close()
	if err != nil {
		return "", refuse("unreadable POST body")
	}
	if int64(len(data)) > MaxRequestBodyBytes {
		return "", refuse("POST body too large")
	}
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	req.ContentLength = int64(len(data))
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
