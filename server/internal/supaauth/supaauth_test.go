package supaauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testSub    = "8f14e45f-ceea-467a-9575-3f1b2c4d5e6f"
	testSecret = "test-only-hs256-secret-0123456789abcdef"
)

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// fakeClock is settable from tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// project is a fake Supabase project: keys and a JWKS endpoint.
type project struct {
	srv     *httptest.Server
	ec      *ecdsa.PrivateKey
	rsa     *rsa.PrivateKey
	hits    atomic.Int32
	mu      sync.Mutex
	body    []byte // JWKS response; nil means the real key set
	status  int
	clock   *fakeClock
	verfier *Verifier
}

func b64u(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func ecJWK(kid string, k *ecdsa.PublicKey) map[string]any {
	raw, _ := k.Bytes() // 0x04 || X || Y
	return map[string]any{"kty": "EC", "crv": "P-256", "alg": "ES256", "use": "sig", "kid": kid, "x": b64u(raw[1:33]), "y": b64u(raw[33:])}
}

func rsaJWK(kid string, k *rsa.PublicKey) map[string]any {
	return map[string]any{"kty": "RSA", "alg": "RS256", "use": "sig", "kid": kid, "n": b64u(k.N.Bytes()), "e": b64u(big.NewInt(int64(k.E)).Bytes())}
}

func newProject(t *testing.T, secret string) *project {
	t.Helper()
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &project{ec: ec, rsa: rk, clock: &fakeClock{t: t0}, status: 200}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		p.hits.Add(1)
		p.mu.Lock()
		body, status := p.body, p.status
		p.mu.Unlock()
		if body == nil {
			body, _ = json.Marshal(map[string]any{"keys": []any{ecJWK("ec-1", &ec.PublicKey), rsaJWK("rsa-1", &rk.PublicKey)}})
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(p.srv.Close)
	v, err := New(Config{ProjectURL: p.srv.URL + "/", JWTSecret: secret, Now: p.clock.Now,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	p.verfier = v
	return p
}

func (p *project) setJWKS(status int, body []byte) {
	p.mu.Lock()
	p.status, p.body = status, body
	p.mu.Unlock()
}

func (p *project) claims() jwt.MapClaims {
	now := p.clock.Now()
	return jwt.MapClaims{
		"iss": p.srv.URL + "/auth/v1", "aud": "authenticated", "sub": testSub, "role": "authenticated",
		"exp": now.Add(time.Hour).Unix(), "iat": now.Unix(),
		"email":         "someone@example.com",
		"user_metadata": map[string]any{"user_name": "octo", "full_name": "Octo Cat"},
	}
}

func sign(t *testing.T, m jwt.SigningMethod, kid string, key any, c jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(m, c)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (p *project) es(t *testing.T, c jwt.MapClaims) string {
	return sign(t, jwt.SigningMethodES256, "ec-1", p.ec, c)
}

func TestVerify(t *testing.T) {
	p := newProject(t, "")
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	with := func(k string, v any) jwt.MapClaims {
		c := p.claims()
		if v == nil {
			delete(c, k)
		} else {
			c[k] = v
		}
		return c
	}
	now := p.clock.Now()
	good := p.es(t, p.claims())
	parts := strings.Split(good, ".")

	cases := []struct {
		name  string
		token string
		ok    bool
	}{
		{"valid ES256", good, true},
		{"valid RS256", sign(t, jwt.SigningMethodRS256, "rsa-1", p.rsa, p.claims()), true},
		{"aud array containing authenticated", p.es(t, with("aud", []string{"other", "authenticated"})), true},
		{"exp within leeway", p.es(t, with("exp", now.Add(-30*time.Second).Unix())), true},
		{"wrong issuer", p.es(t, with("iss", "https://evil.example/auth/v1")), false},
		{"issuer without /auth/v1", p.es(t, with("iss", p.srv.URL)), false},
		{"missing issuer", p.es(t, with("iss", nil)), false},
		{"wrong audience", p.es(t, with("aud", "anon")), false},
		{"missing audience", p.es(t, with("aud", nil)), false},
		{"expired", p.es(t, with("exp", now.Add(-2*time.Minute).Unix())), false},
		{"missing exp", p.es(t, with("exp", nil)), false},
		{"nbf in the future", p.es(t, with("nbf", now.Add(5*time.Minute).Unix())), false},
		{"iat in the future", p.es(t, with("iat", now.Add(5*time.Minute).Unix())), false},
		{"wrong role", p.es(t, with("role", "service_role")), false},
		{"missing role", p.es(t, with("role", nil)), false},
		{"anonymous user", p.es(t, with("is_anonymous", true)), false},
		{"non-UUID sub", p.es(t, with("sub", "github:4242")), false},
		{"upper-case UUID sub", p.es(t, with("sub", strings.ToUpper(testSub))), false},
		{"missing sub", p.es(t, with("sub", nil)), false},
		{"unknown kid", sign(t, jwt.SigningMethodES256, "ec-unknown", p.ec, p.claims()), false},
		{"missing kid", sign(t, jwt.SigningMethodES256, "", p.ec, p.claims()), false},
		{"signed by another key", sign(t, jwt.SigningMethodES256, "ec-1", other, p.claims()), false},
		{"RS256 token naming the EC kid", sign(t, jwt.SigningMethodRS256, "ec-1", p.rsa, p.claims()), false},
		{"ES256 token naming the RSA kid", sign(t, jwt.SigningMethodES256, "rsa-1", p.ec, p.claims()), false},
		{"HS256 without a configured secret", sign(t, jwt.SigningMethodHS256, "", []byte(testSecret), p.claims()), false},
		{"HS256 signed with the RSA public key", sign(t, jwt.SigningMethodHS256, "rsa-1", p.rsa.PublicKey.N.Bytes(), p.claims()), false},
		{"alg none", sign(t, jwt.SigningMethodNone, "ec-1", jwt.UnsafeAllowNoneSignatureType, p.claims()), false},
		{"ES384", sign(t, jwt.SigningMethodES384, "ec-1", mustEC(t, elliptic.P384()), p.claims()), false},
		{"PS256", sign(t, jwt.SigningMethodPS256, "rsa-1", p.rsa, p.claims()), false},
		{"tampered payload", parts[0] + "." + b64u([]byte(strings.Replace(decode(t, parts[1]), testSub, "00000000-0000-4000-8000-000000000000", 1))) + "." + parts[2], false},
		{"stripped signature", parts[0] + "." + parts[1] + ".", false},
		{"garbage", "not.a.jwt", false},
		{"two parts", parts[0] + "." + parts[1], false},
		{"empty", "", false},
		{"huge", strings.Repeat("a", MaxTokenBytes) + ".b.c", false},
	}
	for _, c := range cases {
		id, err := p.verfier.Verify(context.Background(), c.token)
		if c.ok {
			if err != nil || id.Subject != testSub || id.Login != "octo" || id.DisplayName != "Octo Cat" {
				t.Errorf("%s: want ok, got %+v %v", c.name, id, err)
			}
			continue
		}
		t.Logf("%s: %v", c.name, err)
		if err == nil || !errors.Is(err, ErrInvalidToken) {
			t.Errorf("%s: want ErrInvalidToken, got %+v %v", c.name, id, err)
		}
		if err != nil && strings.Contains(err.Error(), testSub) {
			t.Errorf("%s: error leaks a claim: %v", c.name, err)
		}
	}
}

func mustEC(t *testing.T, c elliptic.Curve) *ecdsa.PrivateKey {
	k, err := ecdsa.GenerateKey(c, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func decode(t *testing.T, s string) string {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHS256(t *testing.T) {
	p := newProject(t, testSecret)
	if id, err := p.verfier.Verify(context.Background(), sign(t, jwt.SigningMethodHS256, "", []byte(testSecret), p.claims())); err != nil || id.Subject != testSub {
		t.Fatalf("HS256 with secret: %+v %v", id, err)
	}
	if _, err := p.verfier.Verify(context.Background(), sign(t, jwt.SigningMethodHS256, "", []byte(testSecret+"x"), p.claims())); err == nil {
		t.Fatal("HS256 with the wrong secret accepted")
	}
	// Asymmetric tokens still work alongside the secret.
	if _, err := p.verfier.Verify(context.Background(), p.es(t, p.claims())); err != nil {
		t.Fatalf("ES256 with secret configured: %v", err)
	}
	// Claims are checked for HS256 too.
	c := p.claims()
	c["role"] = "anon"
	if _, err := p.verfier.Verify(context.Background(), sign(t, jwt.SigningMethodHS256, "", []byte(testSecret), c)); err == nil {
		t.Fatal("HS256 anon role accepted")
	}
	if _, err := New(Config{ProjectURL: "https://x.supabase.co", JWTSecret: "short"}); err == nil {
		t.Fatal("short secret accepted")
	}
	if _, err := New(Config{}); err == nil {
		t.Fatal("empty project URL accepted")
	}
}

func TestJWKSCacheAndRefetchLimit(t *testing.T) {
	p := newProject(t, "")
	ctx := context.Background()
	good := p.es(t, p.claims())
	for range 5 {
		if _, err := p.verfier.Verify(ctx, good); err != nil {
			t.Fatal(err)
		}
	}
	if n := p.hits.Load(); n != 1 {
		t.Fatalf("JWKS fetched %d times for one kid, want 1", n)
	}
	// Random kids: at most one refetch per minute.
	for i := range 20 {
		bad := sign(t, jwt.SigningMethodES256, "random-"+string(rune('a'+i)), p.ec, p.claims())
		if _, err := p.verfier.Verify(ctx, bad); !errors.Is(err, ErrUnknownKey) {
			t.Fatalf("unknown kid: %v", err)
		}
	}
	if n := p.hits.Load(); n != 1 {
		t.Fatalf("unknown kids within a minute of a fetch caused %d fetches, want 1", n)
	}
	// A wrong issuer never triggers a fetch.
	p.clock.Advance(2 * time.Minute)
	c := p.claims()
	c["iss"] = "https://evil.example/auth/v1"
	_, _ = p.verfier.Verify(ctx, sign(t, jwt.SigningMethodES256, "random-x", p.ec, c))
	if n := p.hits.Load(); n != 1 {
		t.Fatalf("foreign issuer caused a fetch: %d", n)
	}
	// After a minute, one more refetch for an unknown kid.
	bad := sign(t, jwt.SigningMethodES256, "random-y", p.ec, p.claims())
	for range 5 {
		_, _ = p.verfier.Verify(ctx, bad)
	}
	if n := p.hits.Load(); n != 2 {
		t.Fatalf("after a minute: %d fetches, want 2", n)
	}

	// Key rotation: a new kid appears in the JWKS and is picked up on the
	// next allowed refetch.
	rotated := mustEC(t, elliptic.P256())
	body, _ := json.Marshal(map[string]any{"keys": []any{ecJWK("ec-2", &rotated.PublicKey)}})
	p.setJWKS(200, body)
	p.clock.Advance(2 * time.Minute)
	if _, err := p.verfier.Verify(ctx, sign(t, jwt.SigningMethodES256, "ec-2", rotated, p.claims())); err != nil {
		t.Fatalf("rotated key: %v", err)
	}
	// The old key left the set and stops working.
	if _, err := p.verfier.Verify(ctx, p.es(t, p.claims())); err == nil {
		t.Fatal("key removed from the JWKS still accepted")
	}

	// Cache expiry: after the TTL the set is refetched.
	before := p.hits.Load()
	p.clock.Advance(DefaultCacheTTL + time.Second)
	if _, err := p.verfier.Verify(ctx, sign(t, jwt.SigningMethodES256, "ec-2", rotated, p.claims())); err != nil {
		t.Fatal(err)
	}
	if p.hits.Load() != before+1 {
		t.Fatal("expired cache not refetched")
	}
}

func TestJWKSFailuresKeepOldKeys(t *testing.T) {
	p := newProject(t, "")
	ctx := context.Background()
	if _, err := p.verfier.Verify(ctx, p.es(t, p.claims())); err != nil {
		t.Fatal(err)
	}
	p.setJWKS(500, []byte(`oops`))
	p.clock.Advance(DefaultCacheTTL + time.Minute)
	if _, err := p.verfier.Verify(ctx, p.es(t, p.claims())); err != nil {
		t.Fatalf("stale keys should survive a failing JWKS endpoint: %v", err)
	}
	// Beyond MaxStale the old keys stop working.
	p.clock.Advance(MaxStale)
	if _, err := p.verfier.Verify(ctx, p.es(t, p.claims())); err == nil {
		t.Fatal("keys beyond MaxStale accepted")
	}
}

func TestMalformedJWKS(t *testing.T) {
	big := []byte(`{"keys":[` + strings.Repeat(`{"kty":"EC"},`, MaxJWKSBytes/12) + `{}]}`)
	offCurve := map[string]any{"kty": "EC", "crv": "P-256", "kid": "ec-1", "x": b64u(make([]byte, 32)), "y": b64u(make([]byte, 32))}
	smallRSA, _ := rsa.GenerateKey(rand.Reader, 1024)
	bodies := map[string][]byte{
		"oversized":   big,
		"not json":    []byte(`{"keys":`),
		"no keys":     []byte(`{"foo":[]}`),
		"keys object": []byte(`{"keys":{"kid":"ec-1"}}`),
		"bad keys": mustJSON(map[string]any{"keys": []any{
			"string", 42, nil,
			offCurve,
			map[string]any{"kty": "EC", "crv": "P-256", "kid": "ec-1", "x": "!!", "y": "??"},
			map[string]any{"kty": "EC", "crv": "P-384", "kid": "ec-1", "x": b64u(make([]byte, 48)), "y": b64u(make([]byte, 48))},
			map[string]any{"kty": "EC", "crv": "P-256", "kid": "ec-1", "x": b64u([]byte{1}), "y": b64u([]byte{2})},
			map[string]any{"kty": "RSA", "kid": "ec-1", "n": "", "e": ""},
			rsaJWK("ec-1", &smallRSA.PublicKey),
			map[string]any{"kty": "RSA", "kid": "ec-1", "n": b64u(make([]byte, 256)), "e": b64u([]byte{1, 0, 0, 0, 0, 1})},
			map[string]any{"kty": "oct", "kid": "ec-1", "k": b64u([]byte(testSecret))},
		}}),
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			p := newProject(t, "")
			p.setJWKS(200, body)
			if _, err := p.verfier.Verify(context.Background(), p.es(t, p.claims())); err == nil {
				t.Fatal("token accepted with a malformed JWKS")
			}
		})
	}
	// A good key next to bad ones still works.
	p := newProject(t, "")
	p.setJWKS(200, mustJSON(map[string]any{"keys": []any{offCurve, "x", map[string]any{"kty": "RSA", "kid": "z"}, ecJWK("ec-1", &p.ec.PublicKey)}}))
	if _, err := p.verfier.Verify(context.Background(), p.es(t, p.claims())); err != nil {
		t.Fatalf("good key among bad ones: %v", err)
	}
	// An encryption key with the same kid is ignored.
	p = newProject(t, "")
	enc := ecJWK("ec-1", &p.ec.PublicKey)
	enc["use"] = "enc"
	p.setJWKS(200, mustJSON(map[string]any{"keys": []any{enc}}))
	if _, err := p.verfier.Verify(context.Background(), p.es(t, p.claims())); err == nil {
		t.Fatal("encryption key used for signatures")
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestMetadataIsDisplayOnly(t *testing.T) {
	p := newProject(t, "")
	c := p.claims()
	c["user_metadata"] = map[string]any{"preferred_username": "  pre\x00ferred\n ", "name": strings.Repeat("é", 150), "sub": "00000000-0000-4000-8000-000000000000"}
	id, err := p.verfier.Verify(context.Background(), p.es(t, c))
	if err != nil {
		t.Fatal(err)
	}
	if id.Subject != testSub || id.Login != "preferred" || len([]rune(id.DisplayName)) != 100 {
		t.Fatalf("identity: %+v", id)
	}
	c["user_metadata"] = map[string]any{"user_name": 42, "full_name": []string{"x"}}
	if id, err = p.verfier.Verify(context.Background(), p.es(t, c)); err != nil || id.Login != "" || id.DisplayName != "" {
		t.Fatalf("non-string metadata: %+v %v", id, err)
	}
	delete(c, "user_metadata")
	if _, err = p.verfier.Verify(context.Background(), p.es(t, c)); err != nil {
		t.Fatalf("no metadata: %v", err)
	}
}
