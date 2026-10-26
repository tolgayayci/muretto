// Package broker is the only provider network client in Muretto. It refuses,
// in code and before any network I/O, every request that is not an
// allowlisted read.
//
// What Do refuses before sending anything:
//   - a URL that is not https, carries user info, or has no matching Rule
//     (provider host + method + path pattern + allowed query names);
//   - a host that is not canonical (see CanonicalHost): explicit ports,
//     trailing dots, IP literals and user info are refused, both in requests
//     and in Rules (a Rule with such a host never matches);
//   - a path with an empty, dot or percent-encoded segment;
//   - every method other than GET, except a POST whose body sha256 equals
//     both the Rule's BodySHA256 and a hash declared in Go in pinned.go;
//   - a GET that carries any body (whatever its ContentLength), a request
//     with Transfer-Encoding or trailers, and method-override headers;
//   - anything that mentions Vercel's "decrypt" (path, query name or query
//     value), and the endpoints in deniedPaths (compared case-insensitively),
//     even if a Rule would allow them.
//
// A POST body is read once (at most MaxRequestBodyBytes), hashed, and exactly
// those bytes are sent: Do never sends the caller's reader.
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
	"net/netip"
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
	b := &Broker{allow: canonicalRules(allow)}
	c.CheckRedirect = b.checkRedirect
	b.client = &c
	return b
}

// canonicalRules copies rules with canonical hosts. A rule whose host is not
// canonical (port, trailing dot, IP literal, ...) gets an empty host and so
// never matches: the broker fails closed on a bad rule rather than guessing.
func canonicalRules(in []Rule) []Rule {
	out := make([]Rule, len(in))
	for i, r := range in {
		h, err := CanonicalHost(r.Host)
		if err != nil {
			h = ""
		}
		r.Host = h
		out[i] = r
	}
	return out
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
// the matching rule. For a POST it reads req.Body and replaces it with the
// bytes it read.
func (b *Broker) Check(req *http.Request) (Rule, error) {
	rule, body, err := b.match(req)
	if err != nil {
		return Rule{}, err
	}
	if body != nil {
		setBody(req, body)
	}
	return rule, nil
}

// Do sends req if it matches an allowlist rule, and fails otherwise.
func (b *Broker) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", ErrRefused)
	}
	if req == nil {
		return nil, fmt.Errorf("%w: nil request", ErrRefused)
	}
	rule, body, err := b.match(req)
	if err != nil {
		return nil, err
	}
	// Send a copy whose body is exactly the bytes that were checked (or none).
	req = req.Clone(ctx)
	if body != nil {
		setBody(req, body)
	} else {
		req.Body, req.GetBody, req.ContentLength = nil, nil, 0
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	respBody, err := readCapped(resp.Body, MaxResponseBytes)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	respBody = sanitizeBody(respBody, resp.Header.Get("Content-Type"), rule.Strip)
	if b.observe != nil {
		b.observe(req, resp.StatusCode, respBody)
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	resp.ContentLength = int64(len(respBody))
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
	if _, _, err := b.match(req); err != nil {
		return err
	}
	return nil
}

var safeSegment = regexp.MustCompile(`^[A-Za-z0-9._~@-]+$`)

// hostLabel is one DNS label; topLabel is the last one, which must start with
// a letter so that no dotted-number form ("127.1", "0x7f.1") can pass as a
// name.
var (
	hostLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	topLabel  = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// methodOverrideHeaders let some servers treat a GET as another method.
var methodOverrideHeaders = []string{"X-Http-Method-Override", "X-Http-Method", "X-Method-Override"}

func refuse(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrRefused}, args...)...)
}

// CanonicalHost returns h in the only form the broker compares hosts in: a
// lowercase DNS name with at least two labels. It refuses explicit ports
// (even :443), trailing dots, IP literals, user info, and anything that is
// not plain ASCII letters, digits, '-' and '.'. Adapter loading uses it for
// base_url, and the broker for every Rule and request.
func CanonicalHost(h string) (string, error) {
	if h == "" {
		return "", refuse("empty host")
	}
	if strings.ContainsAny(h, "@:[]%") {
		return "", refuse("host %q has user info, a port or an IP literal", h)
	}
	if strings.HasSuffix(h, ".") {
		return "", refuse("host %q has a trailing dot", h)
	}
	h = strings.ToLower(h)
	if _, err := netip.ParseAddr(h); err == nil {
		return "", refuse("host %q is an IP literal", h)
	}
	labels := strings.Split(h, ".")
	if len(labels) < 2 || len(h) > 253 {
		return "", refuse("host %q is not a DNS name", h)
	}
	for _, l := range labels {
		if !hostLabel.MatchString(l) {
			return "", refuse("host %q is not a DNS name", h)
		}
	}
	if !topLabel.MatchString(labels[len(labels)-1]) {
		return "", refuse("host %q is not a DNS name", h)
	}
	return h, nil
}

func containsDecrypt(s string) bool {
	return strings.Contains(strings.ToLower(s), "decrypt")
}

// match applies every refusal rule and returns the matching Rule. For a POST
// it also returns the body it read and hashed: the caller must send exactly
// these bytes. req.Body is consumed in that case.
func (b *Broker) match(req *http.Request) (Rule, []byte, error) {
	u := req.URL
	if u == nil {
		return Rule{}, nil, refuse("no URL")
	}
	if u.Scheme != "https" {
		return Rule{}, nil, refuse("scheme %q is not https", u.Scheme)
	}
	if u.User != nil {
		return Rule{}, nil, refuse("URL carries credentials")
	}
	if u.Opaque != "" || u.Fragment != "" || u.RawFragment != "" {
		return Rule{}, nil, refuse("unsupported URL form")
	}
	host, err := CanonicalHost(u.Host)
	if err != nil {
		return Rule{}, nil, err
	}
	if req.Host != "" && !strings.EqualFold(req.Host, u.Host) {
		return Rule{}, nil, refuse("Host header differs from URL host")
	}
	path := u.EscapedPath()
	if strings.Contains(path, "%") {
		return Rule{}, nil, refuse("percent-encoded path %q", path)
	}
	if containsDecrypt(path) || containsDecrypt(u.Path) || containsDecrypt(u.RawQuery) {
		return Rule{}, nil, refuse("decrypt is never allowed")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return Rule{}, nil, refuse("malformed query")
	}
	for name, values := range query {
		if containsDecrypt(name) {
			return Rule{}, nil, refuse("decrypt is never allowed")
		}
		for _, v := range values {
			if containsDecrypt(v) {
				return Rule{}, nil, refuse("decrypt is never allowed")
			}
		}
	}
	segs, ok := splitPath(path)
	if !ok {
		return Rule{}, nil, refuse("unsafe path %q", path)
	}
	folded := make([]string, len(segs))
	for i, s := range segs {
		folded[i] = strings.ToLower(s)
	}
	if d := denied(host, folded); d != "" {
		return Rule{}, nil, refuse("endpoint %s is denied in code", d)
	}
	if len(req.TransferEncoding) > 0 || len(req.Trailer) > 0 {
		return Rule{}, nil, refuse("Transfer-Encoding and trailers are never sent")
	}
	for _, h := range methodOverrideHeaders {
		if _, ok := req.Header[h]; ok {
			return Rule{}, nil, refuse("header %s is never sent", h)
		}
	}

	var body []byte
	var bodyHash string
	switch req.Method {
	case http.MethodGet, "":
		// Any body at all is refused, whatever ContentLength claims: a
		// streaming body has ContentLength 0 but is still sent.
		if (req.Body != nil && req.Body != http.NoBody) || req.ContentLength != 0 {
			return Rule{}, nil, refuse("GET with a body")
		}
	case http.MethodPost:
		data, err := readBody(req)
		if err != nil {
			return Rule{}, nil, err
		}
		body = data
		sum := sha256.Sum256(data)
		bodyHash = hex.EncodeToString(sum[:])
	default:
		return Rule{}, nil, refuse("method %s is never allowed", req.Method)
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}

	var lastWhy string
	for _, r := range b.allow {
		if r.Host == "" || r.Host != host || !strings.EqualFold(r.Method, method) {
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
		return r, body, nil
	}
	if lastWhy != "" {
		return Rule{}, nil, refuse("%s", lastWhy)
	}
	return Rule{}, nil, refuse("%s %s%s matches no allowlist rule", method, host, path)
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

// readBody reads a POST body once, up to MaxRequestBodyBytes, and closes it.
func readBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, refuse("POST without a body")
	}
	data, err := io.ReadAll(io.LimitReader(req.Body, MaxRequestBodyBytes+1))
	_ = req.Body.Close()
	req.Body = http.NoBody
	if err != nil {
		return nil, refuse("unreadable POST body")
	}
	if int64(len(data)) > MaxRequestBodyBytes {
		return nil, refuse("POST body too large")
	}
	if len(data) == 0 {
		return nil, refuse("POST without a body")
	}
	return data, nil
}

// setBody makes req carry exactly data.
func setBody(req *http.Request, data []byte) {
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	req.ContentLength = int64(len(data))
}
