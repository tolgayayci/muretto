package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tolgayayci/muretto/server/internal/store"
	"github.com/tolgayayci/muretto/server/internal/supaauth"
)

const (
	tokenPrefix  = "mrt_" // CLI API token
	invitePrefix = "mri_" // org invite
)

// authMode says which credentials a route accepts. Every credential arrives
// as "Authorization: Bearer <credential>"; there are no cookies, so there is
// no CSRF surface.
type authMode int

const (
	// authBearer: a CLI token (mrt_...) only.
	authBearer authMode = iota
	// authUser: a dashboard user, i.e. a Supabase access token. With
	// MURETTO_DEV_LOGIN=1 (loopback only) a CLI token is accepted too, so
	// local development and the demo run without a Supabase project. In
	// production a CLI token is refused here: a stolen CLI token must not be
	// able to approve device logins (minting more tokens) or manage tokens.
	authUser
	// authEither: a CLI token or a Supabase access token.
	authEither
)

// principal is the authenticated caller.
type principal struct {
	user      store.User
	tokenID   string // set when authenticated by a CLI token
	tokenHash string // set when authenticated by a CLI token
}

type authedHandler func(w http.ResponseWriter, r *http.Request, p *principal)

// bearer returns the credential from "Authorization: Bearer <credential>".
func bearer(r *http.Request) (string, bool) {
	scheme, cred, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	cred = strings.TrimSpace(cred)
	return cred, cred != ""
}

// authed authenticates the request according to mode.
func (s *Server) authed(mode authMode, h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearer(r)
		if !ok {
			s.unauthorized(w, r)
			return
		}
		if strings.HasPrefix(raw, tokenPrefix) {
			if mode == authUser && !s.cfg.DevLogin {
				fail(w, r, http.StatusForbidden, "dashboard_auth_required", "this endpoint needs a dashboard sign-in, not a CLI token")
				return
			}
			hash := hashSecret(raw)
			t, u, err := s.st.TokenUser(r.Context(), hash, s.now())
			if err != nil {
				if !errors.Is(err, store.ErrNotFound) {
					s.internal(w, r, err)
					return
				}
				s.unauthorized(w, r)
				return
			}
			h(w, r, &principal{user: u, tokenID: t.ID, tokenHash: hash})
			return
		}
		if mode == authBearer || s.supa == nil {
			s.unauthorized(w, r)
			return
		}
		id, err := s.supa.Verify(r.Context(), raw)
		if err != nil {
			// The error names the failed check, never token contents.
			s.log.Debug("supabase token rejected", "request_id", requestID(r.Context()), "reason", err)
			s.unauthorized(w, r)
			return
		}
		u, err := s.supabaseUser(r, id)
		if err != nil {
			s.internal(w, r, err)
			return
		}
		h(w, r, &principal{user: u})
	}
}

func (s *Server) unauthorized(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="muretto"`)
	fail(w, r, http.StatusUnauthorized, "unauthorized", "authentication required")
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("internal error", "request_id", requestID(r.Context()), "route", r.Pattern, "err", err)
	fail(w, r, http.StatusInternalServerError, "internal", "internal error")
}

// supabaseUser maps a verified Supabase identity to a store user. The
// identity is "supabase:<sub>" only; login and display name come from
// user_metadata, which the user can edit, and are display-only.
func (s *Server) supabaseUser(r *http.Request, id supaauth.Identity) (store.User, error) {
	login := id.Login
	if login == "" {
		login = "user-" + id.Subject[:8]
	}
	return s.users.upsert(r, s, "supabase:"+id.Subject, login, id.DisplayName)
}

// userCache skips the UpsertUser write when a user's login and display name
// have not changed since the last request. A cached entry is exactly what the
// upsert would return: a user's id never changes and only UpsertUser writes
// login and display name.
type userCache struct {
	mu sync.Mutex
	m  map[string]cachedUser
}

type cachedUser struct {
	login, name string
	user        store.User
	at          time.Time
}

const (
	userCacheTTL = 5 * time.Minute
	userCacheMax = 10000
)

func (c *userCache) upsert(r *http.Request, s *Server, identity, login, name string) (store.User, error) {
	now := s.now()
	c.mu.Lock()
	e, ok := c.m[identity]
	c.mu.Unlock()
	if ok && e.login == login && e.name == name && now.Sub(e.at) < userCacheTTL && !now.Before(e.at) {
		return e.user, nil
	}
	u, err := s.st.UpsertUser(r.Context(), identity, login, name, now)
	if err != nil {
		return store.User{}, err
	}
	c.mu.Lock()
	if c.m == nil || len(c.m) >= userCacheMax {
		c.m = map[string]cachedUser{}
	}
	c.m[identity] = cachedUser{login: login, name: name, user: u, at: now}
	c.mu.Unlock()
	return u, nil
}

// DELETE /v1/tokens/self: the CLI logs out.
func (s *Server) revokeSelf(w http.ResponseWriter, r *http.Request, p *principal) {
	if err := s.st.RevokeToken(r.Context(), p.tokenHash, s.now()); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type tokenInfo struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// GET /v1/tokens: the caller's active CLI tokens ("machines"). Hashes are
// never returned.
func (s *Server) listTokens(w http.ResponseWriter, r *http.Request, p *principal) {
	ts, err := s.st.ListTokens(r.Context(), p.user.ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]tokenInfo, 0, len(ts))
	for _, t := range ts {
		ti := tokenInfo{ID: t.ID, Name: t.Name, CreatedAt: t.CreatedAt.UTC().Truncate(time.Second)}
		if !t.LastUsedAt.IsZero() {
			lu := t.LastUsedAt.UTC().Truncate(time.Second)
			ti.LastUsedAt = &lu
		}
		out = append(out, ti)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

var tokenIDPattern = regexp.MustCompile(`^tok_[A-Za-z0-9]{1,64}$`)

// DELETE /v1/tokens/{id}: revoke one of the caller's CLI tokens. Unknown,
// revoked and other users' tokens all get the same 404.
func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request, p *principal) {
	if limited(w, r, s.sessionLimit, "tokens:"+p.user.ID) {
		return
	}
	id := r.PathValue("id")
	if !tokenIDPattern.MatchString(id) {
		fail(w, r, http.StatusNotFound, "not_found", "token not found")
		return
	}
	err := s.st.RevokeUserToken(r.Context(), p.user.ID, id, s.now())
	if errors.Is(err, store.ErrNotFound) {
		fail(w, r, http.StatusNotFound, "not_found", "token not found")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- DEV ONLY: MURETTO_DEV_LOGIN=1, refused by config unless the listen
// address is loopback. These routes are not registered otherwise.

var devLoginName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}$`)

func (s *Server) devUser(r *http.Request, login, name string) (store.User, error) {
	if name == "" {
		name = login
	}
	return s.st.UpsertUser(r.Context(), "dev:"+login, login, truncate(name, 100), s.now())
}

// devToken: POST /v1/dev/token {"login": "alice", "name": "Alice"} -> a CLI token
// without the device flow, for the demo harness. With dev login on, this
// token is also accepted where a dashboard user is required.
func (s *Server) devToken(w http.ResponseWriter, r *http.Request) {
	if limited(w, r, s.devTokenLimit, s.clientIP(r)) {
		return
	}
	var body struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if err := decodeJSON(w, r, 4<<10, &body); err != nil {
		failBody(w, r, err)
		return
	}
	login := strings.ToLower(body.Login)
	if !devLoginName.MatchString(login) {
		fail(w, r, http.StatusBadRequest, "invalid_login", "login must match "+devLoginName.String())
		return
	}
	u, err := s.devUser(r, login, body.Name)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	raw := randomToken(tokenPrefix)
	if err := s.st.CreateToken(r.Context(), store.Token{ID: store.NewID("tok_"), Hash: hashSecret(raw), UserID: u.ID, Name: "dev", CreatedAt: s.now()}); err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{AccessToken: raw, TokenType: "bearer", Login: u.Login})
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Login       string `json:"login"`
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
