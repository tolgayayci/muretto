// Package syncclient is the CLI side of the optional login and sync.
//
// It talks only to Muretto's own sync server, never to a provider, so it uses
// net/http directly rather than the provider broker. It never sends tokens,
// local paths, raw transcripts, full prompts or secret values: every string is
// redacted again (internal/redact) and scrubbed of home-directory paths
// before it leaves the machine. Payload() lists exactly what is sent.
package syncclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
)

// FieldDoc describes one field `muretto sync on` will send.
type FieldDoc struct {
	Entity string
	Field  string
	Note   string
}

// Errors callers can branch on.
var (
	// ErrUnauthorized means the token is missing, unknown or revoked: run `muretto login`.
	ErrUnauthorized = errors.New("sync server rejected the token; run `muretto login` again")
	// ErrAccessDenied means the login was denied on the dashboard.
	ErrAccessDenied = errors.New("login was denied in the browser")
	// ErrExpired means the login code expired before it was approved.
	ErrExpired = errors.New("login code expired; run `muretto login` again")
)

// ServerError is a non-2xx answer from the sync server.
type ServerError struct {
	Status     int
	Code       string // the server's "error" field, e.g. invalid_batch
	Message    string
	Problems   []string
	RetryAfter time.Duration // set on 429
}

func (e *ServerError) Error() string {
	msg := fmt.Sprintf("sync server: %d %s", e.Status, e.Code)
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if len(e.Problems) > 0 {
		msg += " (" + strings.Join(e.Problems, "; ") + ")"
	}
	return msg
}

// requestTimeout bounds every request that has no earlier deadline.
const requestTimeout = 60 * time.Second

var httpClient = &http.Client{Timeout: requestTimeout}

// pollUnit is the unit of the server's poll interval; tests shorten it.
var pollUnit = time.Second

// BuildBatch projects an Answer into a sync batch, skipping entities whose rev
// is unchanged since lastRevs. It returns the new rev map.
func BuildBatch(a contract.Answer, clientVersion string, lastRevs map[string]string) (contract.SyncBatch, map[string]string) {
	b := contract.SyncBatch{
		SchemaVersion: 1,
		BatchID:       newBatchID(),
		SentAt:        time.Now().UTC().Truncate(time.Second),
		ClientVersion: clip(clientVersion, 50),
		Developer:     cleanDeveloper(a.Developer),
		Sessions:      []contract.SyncSession{},
		Resources:     []contract.SyncResource{},
		Attributions:  []contract.SyncAttribution{},
		Findings:      []contract.SyncFinding{},
		Costs:         []contract.SyncCost{},
		Reviews:       []contract.SyncReview{},
		Checks:        []contract.SyncCheck{},
	}
	if b.ClientVersion == "" {
		b.ClientVersion = "unknown"
	}
	next := map[string]string{}
	changed := func(key string, entity any) (contract.Rev, bool) {
		r := rev(entity)
		next[key] = r
		return contract.Rev(r), lastRevs[key] != r
	}
	for _, sv := range a.Sessions {
		e := cleanSession(sv.Session)
		if r, ok := changed("session:"+e.ID, e); ok {
			b.Sessions = append(b.Sessions, contract.SyncSession{Rev: r, Entity: e})
		}
	}
	for _, x := range a.Resources {
		e := cleanResource(x)
		if r, ok := changed("resource:"+e.ID, e); ok {
			b.Resources = append(b.Resources, contract.SyncResource{Rev: r, Entity: e})
		}
	}
	for _, x := range a.Attributions {
		e := cleanAttribution(x)
		if r, ok := changed("attribution:"+e.ResourceID, e); ok {
			b.Attributions = append(b.Attributions, contract.SyncAttribution{Rev: r, Entity: e})
		}
	}
	for _, x := range a.Findings {
		e := cleanFinding(x)
		if r, ok := changed("finding:"+e.ID, e); ok {
			b.Findings = append(b.Findings, contract.SyncFinding{Rev: r, Entity: e})
		}
	}
	for _, x := range a.Costs {
		e := cleanCost(x)
		if r, ok := changed("cost:"+costKey(e), e); ok {
			b.Costs = append(b.Costs, contract.SyncCost{Rev: r, Entity: e})
		}
	}
	for _, x := range a.Reviews {
		e := cleanReview(x)
		if r, ok := changed("review:"+e.ResourceID, e); ok {
			b.Reviews = append(b.Reviews, contract.SyncReview{Rev: r, Entity: e})
		}
	}
	for _, x := range a.Checks {
		e := cleanCheck(x)
		if r, ok := changed("check:"+checkKey(e), e); ok {
			b.Checks = append(b.Checks, contract.SyncCheck{Rev: r, Entity: e})
		}
	}
	return b, next
}

// Empty reports whether a batch carries no entities (nothing changed).
func Empty(b contract.SyncBatch) bool {
	return len(b.Sessions)+len(b.Resources)+len(b.Attributions)+len(b.Findings)+len(b.Costs)+len(b.Reviews)+len(b.Checks) == 0
}

// rev is the hex sha256 of the entity's canonical JSON: encoding/json writes
// struct fields in declaration order and map keys sorted, so equal entities
// always hash equally.
func rev(entity any) string {
	raw, err := json.Marshal(entity)
	if err != nil {
		panic(fmt.Sprintf("syncclient: marshal %T: %v", entity, err)) // contract types always marshal
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func newBatchID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func costKey(c contract.CostRecord) string {
	var interval, start string
	if c.Interval != nil {
		interval = string(*c.Interval)
	}
	if c.PeriodStart != nil {
		start = c.PeriodStart.UTC().Format(time.RFC3339)
	}
	return strings.Join([]string{c.ResourceID, string(c.Source), interval, start}, "|")
}

func checkKey(c contract.ProviderCheck) string {
	return strings.Join([]string{c.Provider, c.AccountID, c.Kind, deref(c.Scope), deref(c.RuleID)}, "|")
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// checkBaseURL refuses to send a token over plain HTTP except to loopback.
func checkBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" {
		return "", fmt.Errorf("invalid sync server URL %q", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		host := u.Hostname()
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return "", fmt.Errorf("sync server URL must use https (plain http is allowed only for localhost)")
		}
	default:
		return "", fmt.Errorf("invalid sync server URL %q", raw)
	}
	return u.String(), nil
}

// call sends a JSON request and decodes a 2xx JSON answer into out.
func call(ctx context.Context, method, base, path, token string, in, out any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, requestTimeout)
		defer cancel()
	}
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "muretto-cli")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sync server unreachable: %w", scrubURLError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		se := &ServerError{Status: resp.StatusCode}
		var e struct {
			Error    string   `json:"error"`
			Message  string   `json:"message"`
			Problems []string `json:"problems"`
			Interval int      `json:"interval"`
		}
		if json.Unmarshal(raw, &e) == nil {
			se.Code, se.Message, se.Problems = e.Error, e.Message, e.Problems
		}
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			se.RetryAfter = time.Duration(s) * time.Second
		}
		return se
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("sync server sent an unexpected answer: %w", err)
		}
	}
	return nil
}

// scrubURLError keeps url.Error's cause but not the URL (it never holds a
// token, but keep error text short and predictable).
func scrubURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// Push uploads a batch.
func Push(ctx context.Context, baseURL, token string, b contract.SyncBatch) error {
	base, err := checkBaseURL(baseURL)
	if err != nil {
		return err
	}
	if token == "" {
		return ErrUnauthorized
	}
	return call(ctx, http.MethodPost, base, "/v1/ingest", token, b, nil)
}

// Revoke revokes the token server-side (`muretto logout`).
func Revoke(ctx context.Context, baseURL, token string) error {
	base, err := checkBaseURL(baseURL)
	if err != nil {
		return err
	}
	return call(ctx, http.MethodDelete, base, "/v1/tokens/self", token, nil, nil)
}

type deviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// Login runs the device flow and returns a Muretto API token. It prints the
// code and URL to out and never prints the token.
func Login(ctx context.Context, baseURL string, out io.Writer) (string, error) {
	base, err := checkBaseURL(baseURL)
	if err != nil {
		return "", err
	}
	host, _ := os.Hostname()
	var dc deviceCode
	if err := call(ctx, http.MethodPost, base, "/v1/device/code", "",
		map[string]string{"client": "muretto-cli", "host": clip(host, 100), "os": runtime.GOOS}, &dc); err != nil {
		return "", err
	}
	if dc.DeviceCode == "" || dc.UserCode == "" || dc.VerificationURI == "" {
		return "", errors.New("sync server sent an incomplete device code")
	}
	link := dc.VerificationURIComplete
	if link == "" {
		link = dc.VerificationURI
	}
	_, _ = fmt.Fprintf(out, "To log in, open this page in your browser:\n\n  %s\n\nand check that it shows the code:  %s\n\nWaiting for approval...\n", link, dc.UserCode)

	interval := max(dc.Interval, 1)
	expires := time.Duration(max(dc.ExpiresIn, 60)) * time.Second
	ctx, cancel := context.WithTimeout(ctx, expires)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "", ErrExpired
			}
			return "", ctx.Err()
		case <-time.After(time.Duration(interval) * pollUnit):
		}
		var tok struct {
			AccessToken string `json:"access_token"`
			Login       string `json:"login"`
		}
		err := call(ctx, http.MethodPost, base, "/v1/device/token", "",
			map[string]string{"device_code": dc.DeviceCode, "grant_type": "urn:ietf:params:oauth:grant-type:device_code"}, &tok)
		var se *ServerError
		switch {
		case err == nil:
			if !strings.HasPrefix(tok.AccessToken, "mrt_") {
				return "", errors.New("sync server sent an unexpected token")
			}
			if tok.Login != "" {
				_, _ = fmt.Fprintf(out, "Logged in as %s.\n", tok.Login)
			}
			return tok.AccessToken, nil
		case errors.As(err, &se) && se.Code == "authorization_pending":
		case errors.As(err, &se) && se.Code == "slow_down":
			interval += 5 // RFC 8628 §3.5
		case errors.As(err, &se) && se.Code == "access_denied":
			return "", ErrAccessDenied
		case errors.As(err, &se) && se.Code == "expired_token":
			return "", ErrExpired
		case errors.As(err, &se) && se.Status == http.StatusTooManyRequests:
			interval += 5
		default:
			if ctx.Err() != nil {
				continue // reported by the select above
			}
			return "", err
		}
	}
}
