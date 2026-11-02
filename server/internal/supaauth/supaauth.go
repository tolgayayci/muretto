// Package supaauth verifies Supabase Auth access tokens (JWTs) for the sync
// API. The dashboard's server code forwards the signed-in user's access token
// as "Authorization: Bearer <jwt>"; this package checks it and returns the
// user's stable id.
//
// What is checked, in order:
//
//   - the token is at most MaxTokenBytes and has three parts;
//   - the algorithm is ES256 or RS256, or HS256 only when a legacy JWT secret
//     is configured. "none" and everything else are rejected;
//   - asymmetric tokens carry a kid that names a key in the project's JWKS
//     (<project>/auth/v1/.well-known/jwks.json), and the key type matches the
//     algorithm;
//   - the signature;
//   - iss is exactly <project>/auth/v1, aud contains "authenticated", exp is
//     present and in the future, nbf and iat (when present) are not in the
//     future, all with a 60 s leeway;
//   - sub is a lower-case UUID, role is "authenticated", and the user is not an
//     anonymous Supabase user.
//
// user_metadata is editable by the user (supabase.auth.updateUser), so it is
// only ever used for display: Identity.Login and Identity.DisplayName. The
// account is keyed by sub alone.
//
// Nothing here logs or returns token contents. Error messages name the check
// that failed, never claim values.
package supaauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
)

// Defaults. Config fields override them (tests use small values).
const (
	DefaultCacheTTL     = 10 * time.Minute // how long a fetched JWKS is fresh
	DefaultMinRefetch   = time.Minute      // at most one JWKS fetch per this interval
	DefaultFetchTimeout = 5 * time.Second
	// MaxStale bounds how long keys from the last good fetch keep working
	// while the JWKS endpoint is failing.
	MaxStale = time.Hour
	// Leeway tolerates clock skew between Supabase and this server.
	Leeway = 60 * time.Second
	// MaxTokenBytes rejects absurd Authorization headers before parsing.
	MaxTokenBytes = 16 << 10
	// MaxJWKSBytes caps the JWKS response body.
	MaxJWKSBytes = 256 << 10
	// maxKeys caps how many keys one JWKS may contribute.
	maxKeys = 32
	// MinSecretBytes is the shortest accepted legacy HS256 secret.
	MinSecretBytes = 32
)

// Audience and Role are what Supabase puts in a signed-in user's token.
const (
	Audience = "authenticated"
	Role     = "authenticated"
)

// Errors. ErrInvalidToken wraps every rejection of the token itself;
// ErrUnknownKey means the kid is not (yet) in the JWKS.
var (
	ErrInvalidToken = errors.New("invalid supabase token")
	ErrUnknownKey   = errors.New("signing key not found in JWKS")
)

// Config configures a Verifier.
type Config struct {
	// ProjectURL is the Supabase project URL, e.g. https://abcd.supabase.co.
	// Issuer and JWKS URL derive from it. Validation of the URL itself
	// (https, no path) is the caller's job; see server/internal/config.
	ProjectURL string
	// JWTSecret enables legacy HS256 tokens. Empty: HS256 is rejected.
	JWTSecret string

	HTTPClient   *http.Client     // default: a client with FetchTimeout
	Now          func() time.Time // default time.Now
	Logger       *slog.Logger     // JWKS fetch problems only; default slog.Default()
	CacheTTL     time.Duration    // default DefaultCacheTTL
	MinRefetch   time.Duration    // default DefaultMinRefetch
	FetchTimeout time.Duration    // default DefaultFetchTimeout
}

// Identity is a verified Supabase user.
type Identity struct {
	// Subject is the Supabase user id (a UUID). It is the only field that
	// identifies the user.
	Subject string
	// Login and DisplayName come from user_metadata: display only.
	Login       string
	DisplayName string
}

// Verifier checks Supabase access tokens. It is safe for concurrent use.
type Verifier struct {
	issuer  string
	jwksURL string
	secret  []byte
	methods []string
	parser  *jwt.Parser

	client       *http.Client
	now          func() time.Time
	log          *slog.Logger
	cacheTTL     time.Duration
	minRefetch   time.Duration
	fetchTimeout time.Duration

	mu          sync.Mutex // guards the fields below; held during a fetch
	keys        map[string]publicKey
	fetchedAt   time.Time // last successful fetch
	lastAttempt time.Time // last fetch attempt, successful or not
}

type publicKey struct {
	alg string // ES256 or RS256
	key any    // *ecdsa.PublicKey or *rsa.PublicKey
}

// New builds a Verifier.
func New(c Config) (*Verifier, error) {
	base := strings.TrimRight(c.ProjectURL, "/")
	if base == "" {
		return nil, errors.New("supaauth: ProjectURL is required")
	}
	if c.JWTSecret != "" && len(c.JWTSecret) < MinSecretBytes {
		return nil, fmt.Errorf("supaauth: JWT secret must be at least %d bytes", MinSecretBytes)
	}
	v := &Verifier{
		issuer:       base + "/auth/v1",
		jwksURL:      base + "/auth/v1/.well-known/jwks.json",
		client:       c.HTTPClient,
		now:          c.Now,
		log:          c.Logger,
		cacheTTL:     c.CacheTTL,
		minRefetch:   c.MinRefetch,
		fetchTimeout: c.FetchTimeout,
		methods:      []string{"ES256", "RS256"},
	}
	if v.now == nil {
		v.now = time.Now
	}
	if v.log == nil {
		v.log = slog.Default()
	}
	if v.cacheTTL <= 0 {
		v.cacheTTL = DefaultCacheTTL
	}
	if v.minRefetch <= 0 {
		v.minRefetch = DefaultMinRefetch
	}
	if v.fetchTimeout <= 0 {
		v.fetchTimeout = DefaultFetchTimeout
	}
	if v.client == nil {
		v.client = &http.Client{Timeout: v.fetchTimeout}
	}
	if c.JWTSecret != "" {
		v.secret = []byte(c.JWTSecret)
		v.methods = append(v.methods, "HS256")
	}
	v.parser = jwt.NewParser(
		jwt.WithValidMethods(v.methods),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(Audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(Leeway),
		jwt.WithTimeFunc(func() time.Time { return v.now() }),
	)
	return v, nil
}

// Issuer is the expected iss claim.
func (v *Verifier) Issuer() string { return v.issuer }

// claims is the part of a Supabase access token this server reads.
type claims struct {
	jwt.RegisteredClaims
	Role         string         `json:"role"`
	IsAnonymous  bool           `json:"is_anonymous"`
	UserMetadata map[string]any `json:"user_metadata"`
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Verify checks raw and returns the user it was issued to.
func (v *Verifier) Verify(ctx context.Context, raw string) (Identity, error) {
	if raw == "" || len(raw) > MaxTokenBytes || strings.Count(raw, ".") != 2 {
		return Identity{}, fmt.Errorf("%w: malformed", ErrInvalidToken)
	}
	var c claims
	_, err := v.parser.ParseWithClaims(raw, &c, func(t *jwt.Token) (any, error) { return v.keyFor(ctx, t) })
	if err != nil {
		if errors.Is(err, ErrUnknownKey) {
			return Identity{}, fmt.Errorf("%w: %w", ErrInvalidToken, ErrUnknownKey)
		}
		return Identity{}, fmt.Errorf("%w: %s", ErrInvalidToken, reason(err))
	}
	if !uuidRe.MatchString(c.Subject) {
		return Identity{}, fmt.Errorf("%w: sub is not a UUID", ErrInvalidToken)
	}
	if c.Role != Role {
		return Identity{}, fmt.Errorf("%w: role is not %s", ErrInvalidToken, Role)
	}
	if c.IsAnonymous {
		return Identity{}, fmt.Errorf("%w: anonymous users are not accepted", ErrInvalidToken)
	}
	login := metaString(c.UserMetadata, "user_name", "preferred_username")
	name := metaString(c.UserMetadata, "full_name", "name")
	return Identity{Subject: c.Subject, Login: login, DisplayName: name}, nil
}

// reason maps a jwt error to a short, value-free description.
func reason(err error) string {
	switch {
	case errors.Is(err, jwt.ErrTokenMalformed):
		return "malformed"
	case errors.Is(err, jwt.ErrTokenSignatureInvalid):
		return "bad signature or algorithm"
	case errors.Is(err, jwt.ErrTokenExpired):
		return "expired"
	case errors.Is(err, jwt.ErrTokenNotValidYet):
		return "not valid yet"
	case errors.Is(err, jwt.ErrTokenUsedBeforeIssued):
		return "issued in the future"
	case errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return "wrong issuer"
	case errors.Is(err, jwt.ErrTokenInvalidAudience):
		return "wrong audience"
	case errors.Is(err, jwt.ErrTokenRequiredClaimMissing):
		return "required claim missing"
	case errors.Is(err, jwt.ErrTokenUnverifiable):
		return "unverifiable"
	}
	return "rejected"
}

// keyFor is the jwt Keyfunc. It runs before the signature is checked, so it
// treats every header and claim as attacker-controlled.
func (v *Verifier) keyFor(ctx context.Context, t *jwt.Token) (any, error) {
	alg, _ := t.Header["alg"].(string)
	// Cheap pre-check so tokens from another issuer never trigger a JWKS
	// fetch. The parser checks iss again after the signature.
	if iss, err := t.Claims.GetIssuer(); err != nil || iss != v.issuer {
		return nil, jwt.ErrTokenInvalidIssuer
	}
	switch alg {
	case "HS256":
		if v.secret == nil { // unreachable: WithValidMethods excludes HS256
			return nil, errors.New("HS256 is not enabled")
		}
		return v.secret, nil
	case "ES256", "RS256":
		kid, ok := t.Header["kid"].(string)
		if !ok || kid == "" || len(kid) > 200 {
			return nil, errors.New("kid header is required")
		}
		k, err := v.lookup(ctx, kid)
		if err != nil {
			return nil, err
		}
		if k.alg != alg {
			return nil, errors.New("key type does not match alg")
		}
		return k.key, nil
	}
	return nil, errors.New("algorithm not allowed")
}

// lookup returns the key for kid, fetching the JWKS when the cache is stale
// or the kid is unknown. Fetches happen at most once per minRefetch, so a
// stream of tokens with random kids cannot make us hammer Supabase.
func (v *Verifier) lookup(ctx context.Context, kid string) (publicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.now()
	k, have := v.keys[kid]
	fresh := !v.fetchedAt.IsZero() && now.Sub(v.fetchedAt) < v.cacheTTL
	if have && fresh {
		return k, nil
	}
	if v.lastAttempt.IsZero() || now.Sub(v.lastAttempt) >= v.minRefetch {
		v.lastAttempt = now
		// The fetch outlives a cancelled request so one impatient client
		// cannot spend the refetch budget for everyone.
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), v.fetchTimeout)
		keys, err := v.fetch(fctx)
		cancel()
		if err != nil {
			v.log.Warn("supabase JWKS fetch failed", "err", err)
		} else {
			v.keys, v.fetchedAt = keys, now
		}
		k, have = v.keys[kid]
	}
	if !have {
		return publicKey{}, ErrUnknownKey
	}
	if now.Sub(v.fetchedAt) > v.cacheTTL+MaxStale {
		return publicKey{}, errors.New("JWKS is stale and cannot be refreshed")
	}
	return k, nil
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// fetch downloads and parses the JWKS. Keys it cannot use are skipped; a
// response that is not a JWKS at all is an error and keeps the old keys.
func (v *Verifier) fetch(ctx context.Context) (map[string]publicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxJWKSBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if len(body) > MaxJWKSBytes {
		return nil, errors.New("response too large")
	}
	var set struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil || set.Keys == nil {
		return nil, errors.New("response is not a JWKS")
	}
	keys := map[string]publicKey{}
	for _, rawKey := range set.Keys {
		if len(keys) >= maxKeys {
			break
		}
		var j jwk
		if json.Unmarshal(rawKey, &j) != nil {
			continue
		}
		k, err := parseJWK(j)
		if err != nil {
			continue
		}
		if _, dup := keys[j.Kid]; dup {
			continue // ambiguous kid: keep the first
		}
		keys[j.Kid] = k
	}
	return keys, nil
}

// parseJWK converts one public signing key. Only P-256 EC keys (ES256) and
// 2048 to 8192-bit RSA keys (RS256) are accepted.
func parseJWK(j jwk) (publicKey, error) {
	if j.Kid == "" || len(j.Kid) > 200 {
		return publicKey{}, errors.New("missing kid")
	}
	if j.Use != "" && j.Use != "sig" {
		return publicKey{}, errors.New("not a signing key")
	}
	switch j.Kty {
	case "EC":
		if j.Crv != "P-256" || (j.Alg != "" && j.Alg != "ES256") {
			return publicKey{}, errors.New("unsupported EC key")
		}
		x, err1 := b64(j.X)
		y, err2 := b64(j.Y)
		if err1 != nil || err2 != nil || len(x) != 32 || len(y) != 32 {
			return publicKey{}, errors.New("bad EC coordinates")
		}
		// ParseUncompressedPublicKey rejects points that are not on the curve.
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, x...), y...))
		if err != nil {
			return publicKey{}, err
		}
		return publicKey{alg: "ES256", key: pub}, nil
	case "RSA":
		if j.Alg != "" && j.Alg != "RS256" {
			return publicKey{}, errors.New("unsupported RSA alg")
		}
		n, err1 := b64(j.N)
		e, err2 := b64(j.E)
		if err1 != nil || err2 != nil || len(n) == 0 || len(e) == 0 || len(e) > 4 {
			return publicKey{}, errors.New("bad RSA parameters")
		}
		mod := new(big.Int).SetBytes(n)
		exp := new(big.Int).SetBytes(e)
		if bits := mod.BitLen(); bits < 2048 || bits > 8192 {
			return publicKey{}, errors.New("RSA key size out of range")
		}
		if !exp.IsInt64() || exp.Int64() < 3 || exp.Int64() > 1<<31-1 || exp.Bit(0) == 0 {
			return publicKey{}, errors.New("bad RSA exponent")
		}
		return publicKey{alg: "RS256", key: &rsa.PublicKey{N: mod, E: int(exp.Int64())}}, nil
	}
	return publicKey{}, errors.New("unsupported key type")
}

func b64(s string) ([]byte, error) { return base64.RawURLEncoding.DecodeString(s) }

// metaString returns the first non-empty string among keys, sanitized for
// display: printable characters only, trimmed, at most 100 runes.
func metaString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		s, _ := m[k].(string)
		if s = Clean(s); s != "" {
			return s
		}
	}
	return ""
}

// Clean keeps printable characters, trims space and caps at 100 runes.
func Clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 100 {
		s = strings.TrimSpace(string(r[:100]))
	}
	return s
}
