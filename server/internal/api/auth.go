package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/tolgayayci/muretto/server/internal/store"
)

const (
	sessionCookie = "mrt_session"
	oauthCookie   = "mrt_oauth"
	csrfHeader    = "X-CSRF-Token"

	tokenPrefix   = "mrt_" // CLI API token
	sessionPrefix = "mrs_" // dashboard session
	invitePrefix  = "mri_" // org invite
)

type authMode int

const (
	authBearer authMode = iota
	authCookie
	authEither
)

// principal is the authenticated caller.
type principal struct {
	user         store.User
	viaCookie    bool
	sessionToken string // raw cookie value, only to derive the CSRF token
	tokenHash    string
}

type authedHandler func(w http.ResponseWriter, r *http.Request, p *principal)

// authed authenticates the request. Cookie-authenticated state-changing
// requests must carry the CSRF token in X-CSRF-Token.
func (s *Server) authed(mode authMode, h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authz := r.Header.Get("Authorization")
		if authz != "" && mode != authCookie {
			raw, ok := strings.CutPrefix(authz, "Bearer ")
			if !ok || !strings.HasPrefix(raw, tokenPrefix) {
				s.unauthorized(w, r, true)
				return
			}
			hash := hashSecret(raw)
			_, u, err := s.st.TokenUser(r.Context(), hash, s.now())
			if err != nil {
				if !errors.Is(err, store.ErrNotFound) {
					s.internal(w, r, err)
					return
				}
				s.unauthorized(w, r, true)
				return
			}
			h(w, r, &principal{user: u, tokenHash: hash})
			return
		}
		if mode == authBearer {
			s.unauthorized(w, r, true)
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil || !strings.HasPrefix(c.Value, sessionPrefix) {
			s.unauthorized(w, r, false)
			return
		}
		u, err := s.st.WebSessionUser(r.Context(), hashSecret(c.Value), s.now())
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				s.internal(w, r, err)
				return
			}
			s.unauthorized(w, r, false)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			got := r.Header.Get(csrfHeader)
			if subtle.ConstantTimeCompare([]byte(got), []byte(csrfToken(c.Value))) != 1 {
				fail(w, r, http.StatusForbidden, "csrf_failed", "missing or invalid "+csrfHeader+" header")
				return
			}
		}
		h(w, r, &principal{user: u, viaCookie: true, sessionToken: c.Value})
	}
}

// csrfToken derives the CSRF token from the session secret. It is returned by
// GET /v1/me and cannot be turned back into the session.
func csrfToken(session string) string {
	m := hmac.New(sha256.New, []byte(session))
	m.Write([]byte("muretto-csrf-v1"))
	return hex.EncodeToString(m.Sum(nil))
}

func (s *Server) unauthorized(w http.ResponseWriter, r *http.Request, bearer bool) {
	if bearer {
		w.Header().Set("WWW-Authenticate", `Bearer realm="muretto"`)
	}
	fail(w, r, http.StatusUnauthorized, "unauthorized", "authentication required")
}

func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("internal error", "request_id", requestID(r.Context()), "route", r.Pattern, "err", err)
	fail(w, r, http.StatusInternalServerError, "internal", "internal error")
}

// startSession creates a dashboard session and sets the cookie.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u store.User) bool {
	tok := randomToken(sessionPrefix)
	now := s.now()
	if err := s.st.CreateWebSession(r.Context(), hashSecret(tok), u.ID, now.Add(s.opt.SessionTTL), now); err != nil {
		s.internal(w, r, err)
		return false
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/", MaxAge: int(s.opt.SessionTTL.Seconds()),
		HttpOnly: true, Secure: s.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
	return true
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, p *principal) {
	if err := s.st.DeleteWebSession(r.Context(), hashSecret(p.sessionToken)); err != nil {
		s.internal(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) revokeSelf(w http.ResponseWriter, r *http.Request, p *principal) {
	if err := s.st.RevokeToken(r.Context(), p.tokenHash, s.now()); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// safeReturnTo accepts only a same-site relative path, defeating open redirects.
func safeReturnTo(v string) string {
	if v == "" || len(v) > 512 || v[0] != '/' || strings.HasPrefix(v, "//") || strings.ContainsAny(v, "\\\r\n\t") {
		return "/"
	}
	return v
}

// --- GitHub OAuth web flow (PKCE + state). The GitHub token is used once to
// read /user and then dropped; it is never stored or logged.

func (s *Server) githubLogin(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.GitHubConfigured() {
		fail(w, r, http.StatusServiceUnavailable, "github_not_configured", "GitHub login is not configured on this server")
		return
	}
	state := randomToken("")
	verifier := randomToken("")
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	returnTo := safeReturnTo(r.URL.Query().Get("return_to"))
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: state + "." + verifier + "." + base64.RawURLEncoding.EncodeToString([]byte(returnTo)),
		Path: "/", MaxAge: 600, HttpOnly: true, Secure: s.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
	q := url.Values{
		"client_id":             {s.cfg.GitHubClientID},
		"redirect_uri":          {s.cfg.PublicURL + "/auth/github/callback"},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"allow_signup":          {"true"},
	}
	http.Redirect(w, r, s.opt.GitHubOAuthURL+"/login/oauth/authorize?"+q.Encode(), http.StatusFound)
}

func (s *Server) loginFailed(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, s.cfg.DashboardURL+"/login?error="+url.QueryEscape(reason), http.StatusFound)
}

func (s *Server) githubCallback(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.GitHubConfigured() {
		fail(w, r, http.StatusServiceUnavailable, "github_not_configured", "GitHub login is not configured on this server")
		return
	}
	c, err := r.Cookie(oauthCookie)
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.SecureCookies(), SameSite: http.SameSiteLaxMode})
	if err != nil {
		s.loginFailed(w, r, "expired")
		return
	}
	parts := strings.Split(c.Value, ".")
	q := r.URL.Query()
	if len(parts) != 3 || q.Get("state") == "" || subtle.ConstantTimeCompare([]byte(parts[0]), []byte(q.Get("state"))) != 1 {
		s.loginFailed(w, r, "state")
		return
	}
	if q.Get("error") != "" || q.Get("code") == "" {
		s.loginFailed(w, r, "denied")
		return
	}
	rt, _ := base64.RawURLEncoding.DecodeString(parts[2])
	returnTo := safeReturnTo(string(rt))

	gh, err := s.githubUser(r, q.Get("code"), parts[1])
	if err != nil {
		s.log.Warn("github login failed", "request_id", requestID(r.Context()), "err", err)
		s.loginFailed(w, r, "github")
		return
	}
	name := gh.Name
	if name == "" {
		name = gh.Login
	}
	u, err := s.st.UpsertUser(r.Context(), "github:"+strconv.FormatInt(gh.ID, 10), gh.Login, truncate(name, 100), s.now())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if s.startSession(w, r, u) {
		http.Redirect(w, r, s.cfg.DashboardURL+returnTo, http.StatusFound)
	}
}

type githubProfile struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

// githubUser exchanges the code and reads /user. The access token stays in
// this function and is discarded on return.
func (s *Server) githubUser(r *http.Request, code, verifier string) (githubProfile, error) {
	form := url.Values{
		"client_id":     {s.cfg.GitHubClientID},
		"client_secret": {s.cfg.GitHubClientSecret},
		"code":          {code},
		"redirect_uri":  {s.cfg.PublicURL + "/auth/github/callback"},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.opt.GitHubOAuthURL+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return githubProfile{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.opt.HTTPClient.Do(req)
	if err != nil {
		return githubProfile{}, fmt.Errorf("token exchange: %w", err)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&tok)
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || tok.AccessToken == "" {
		return githubProfile{}, fmt.Errorf("token exchange: status %d error %q", resp.StatusCode, tok.Error)
	}

	req, err = http.NewRequestWithContext(r.Context(), http.MethodGet, s.opt.GitHubAPIURL+"/user", nil)
	if err != nil {
		return githubProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err = s.opt.HTTPClient.Do(req)
	if err != nil {
		return githubProfile{}, fmt.Errorf("read user: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var p githubProfile
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&p); err != nil || resp.StatusCode != http.StatusOK || p.ID == 0 || p.Login == "" {
		return githubProfile{}, fmt.Errorf("read user: status %d", resp.StatusCode)
	}
	return p, nil
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

// devLogin: GET /auth/dev/login?login=alice&name=Alice&return_to=/
func (s *Server) devLogin(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	login := strings.ToLower(q.Get("login"))
	if !devLoginName.MatchString(login) {
		fail(w, r, http.StatusBadRequest, "invalid_login", "login must match "+devLoginName.String())
		return
	}
	u, err := s.devUser(r, login, q.Get("name"))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if s.startSession(w, r, u) {
		http.Redirect(w, r, s.cfg.DashboardURL+safeReturnTo(q.Get("return_to")), http.StatusFound)
	}
}

// devToken: POST /v1/dev/token {"login": "alice", "name": "Alice"} -> a CLI token
// without the device flow, for the demo harness.
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
