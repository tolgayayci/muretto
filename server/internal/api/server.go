// Package api is the Muretto sync API: login (GitHub web OAuth for the
// dashboard, a device flow for the CLI), orgs and invites, ingest, answer
// views and the SSE live channel. See docs/ARCHITECTURE.md "Sync API".
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tolgayayci/muretto/server/internal/config"
	"github.com/tolgayayci/muretto/server/internal/events"
	"github.com/tolgayayci/muretto/server/internal/ratelimit"
	"github.com/tolgayayci/muretto/server/internal/store"
	"github.com/tolgayayci/muretto/server/internal/validate"
)

// Options wires a Server.
type Options struct {
	Config config.Config
	Store  store.Store
	Hub    *events.Hub
	// PublishLocal publishes ingest events to Hub directly. Set it with the
	// memory store; with Postgres, events arrive through LISTEN/NOTIFY.
	PublishLocal bool
	Logger       *slog.Logger
	Now          func() time.Time

	HTTPClient     *http.Client // for GitHub calls
	GitHubOAuthURL string       // default https://github.com
	GitHubAPIURL   string       // default https://api.github.com

	DevicePollInterval time.Duration // default 5 s
	DeviceTTL          time.Duration // default 15 min
	SessionTTL         time.Duration // default 30 days
	InviteTTL          time.Duration // default 7 days
	Heartbeat          time.Duration // SSE heartbeat, default 20 s
}

// Server is the HTTP API.
type Server struct {
	opt       Options
	cfg       config.Config
	st        store.Store
	hub       *events.Hub
	log       *slog.Logger
	now       func() time.Time
	validator *validate.Validator

	ingestLimit   *ratelimit.Limiter // per token
	requestLimit  *ratelimit.Limiter // provider requests, per IP
	deviceLimit   *ratelimit.Limiter // device code + token, per IP
	sessionLimit  *ratelimit.Limiter // approve / invite accept / device lookup, per session
	devTokenLimit *ratelimit.Limiter
}

// New builds a Server.
func New(o Options) (*Server, error) {
	v, err := validate.New()
	if err != nil {
		return nil, err
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if o.GitHubOAuthURL == "" {
		o.GitHubOAuthURL = "https://github.com"
	}
	if o.GitHubAPIURL == "" {
		o.GitHubAPIURL = "https://api.github.com"
	}
	if o.DevicePollInterval == 0 {
		o.DevicePollInterval = 5 * time.Second
	}
	if o.DeviceTTL == 0 {
		o.DeviceTTL = 15 * time.Minute
	}
	if o.SessionTTL == 0 {
		o.SessionTTL = 30 * 24 * time.Hour
	}
	if o.InviteTTL == 0 {
		o.InviteTTL = 7 * 24 * time.Hour
	}
	if o.Heartbeat == 0 {
		o.Heartbeat = 20 * time.Second
	}
	if o.Hub == nil {
		o.Hub = events.NewHub()
	}
	perMin := max(o.Config.IngestPerMinute, 1)
	return &Server{
		opt: o, cfg: o.Config, st: o.Store, hub: o.Hub, log: o.Logger, now: o.Now, validator: v,
		ingestLimit:   ratelimit.New(perMin, time.Minute, min(perMin, 10)).WithClock(o.Now),
		requestLimit:  ratelimit.New(5, time.Hour, 3).WithClock(o.Now),
		deviceLimit:   ratelimit.New(60, time.Minute, 20).WithClock(o.Now),
		sessionLimit:  ratelimit.New(20, time.Minute, 10).WithClock(o.Now),
		devTokenLimit: ratelimit.New(60, time.Minute, 20).WithClock(o.Now),
	}, nil
}

// Handler returns the routed, middleware-wrapped handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })

	// CLI login: device flow.
	mux.HandleFunc("POST /v1/device/code", s.deviceCode)
	mux.HandleFunc("POST /v1/device/token", s.deviceToken)
	mux.HandleFunc("GET /v1/device", s.authed(authCookie, s.deviceLookup))
	mux.HandleFunc("POST /v1/device/approve", s.authed(authCookie, s.deviceApprove))
	mux.HandleFunc("DELETE /v1/tokens/self", s.authed(authBearer, s.revokeSelf))

	// Dashboard login.
	mux.HandleFunc("GET /auth/github/login", s.githubLogin)
	mux.HandleFunc("GET /auth/github/callback", s.githubCallback)
	mux.HandleFunc("POST /auth/logout", s.authed(authCookie, s.logout))
	if s.cfg.DevLogin {
		mux.HandleFunc("GET /auth/dev/login", s.devLogin)
		mux.HandleFunc("POST /v1/dev/token", s.devToken)
	}

	mux.HandleFunc("GET /v1/me", s.authed(authEither, s.me))
	mux.HandleFunc("POST /v1/ingest", s.authed(authBearer, s.ingest))

	mux.HandleFunc("POST /v1/orgs", s.authed(authCookie, s.createOrg))
	mux.HandleFunc("GET /v1/orgs/{id}", s.authed(authCookie, s.getOrg))
	mux.HandleFunc("POST /v1/orgs/{id}/invites", s.authed(authCookie, s.createInvite))
	mux.HandleFunc("POST /v1/invites/{token}/accept", s.authed(authCookie, s.acceptInvite))

	mux.HandleFunc("GET /v1/answer", s.authed(authEither, s.answerMe))
	mux.HandleFunc("GET /v1/orgs/{id}/answer", s.authed(authCookie, s.answerTeam))
	mux.HandleFunc("GET /v1/orgs/{id}/events", s.authed(authCookie, s.events))

	mux.HandleFunc("POST /v1/provider-requests", s.providerRequest)

	return s.withRequestID(s.withRecover(s.withLogging(s.withCORS(s.withSecurityHeaders(mux)))))
}

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if !validRequestID.MatchString(id) {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func (s *Server) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.log.Error("panic", "request_id", requestID(r.Context()), "panic", v)
				fail(w, r, http.StatusInternalServerError, "internal", "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach Flush and SetWriteDeadline.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// withLogging logs one line per request. It logs the route pattern, never the
// raw path or query: those can carry invite tokens and OAuth codes.
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		route := r.Pattern
		if route == "" {
			route = r.Method + " (unmatched)"
		}
		s.log.Info("request", "request_id", requestID(r.Context()), "route", route, "status", sw.status,
			"duration_ms", s.now().Sub(start).Milliseconds())
	})
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		w.Header().Add("Vary", "Origin")
		if origin != "" && origin == s.cfg.DashboardURL {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Expose-Headers", "X-Request-Id, Retry-After")
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE")
				h.Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, Last-Event-ID")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		} else if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cache-Control", "no-store")
		if s.cfg.SecureCookies() {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP is the peer address, or with MURETTO_TRUST_PROXY=1 the right-most
// X-Forwarded-For entry (the one the trusted proxy appended).
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limited applies a limiter and writes 429 with Retry-After when exhausted.
func limited(w http.ResponseWriter, r *http.Request, l *ratelimit.Limiter, key string) bool {
	ok, wait := l.Allow(key)
	if ok {
		return false
	}
	secs := int(wait.Seconds()) + 1
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	fail(w, r, http.StatusTooManyRequests, "rate_limited", "too many requests; retry later")
	return true
}
