package api

import (
	"crypto/rand"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/tolgayayci/muretto/server/internal/store"
)

// userCodeAlphabet has no vowels (no accidental words) and no look-alikes.
const userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

func newUserCode() string {
	out := make([]byte, 0, 8)
	buf := make([]byte, 1)
	for len(out) < 8 {
		_, _ = rand.Read(buf)
		if int(buf[0]) >= 256-256%len(userCodeAlphabet) {
			continue // rejection sampling keeps the distribution uniform
		}
		out = append(out, userCodeAlphabet[int(buf[0])%len(userCodeAlphabet)])
	}
	return string(out)
}

// normalizeUserCode accepts "wdjb-mjht", "WDJB MJHT", "WDJBMJHT".
func normalizeUserCode(s string) (string, bool) {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(s))
	if len(s) != 8 {
		return "", false
	}
	for _, c := range s {
		if !strings.ContainsRune(userCodeAlphabet, c) {
			return "", false
		}
	}
	return s, true
}

func displayUserCode(c string) string { return c[:4] + "-" + c[4:] }

func cleanClientField(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, s)
	return truncate(strings.TrimSpace(s), 100)
}

type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

func ceilSeconds(d time.Duration) int { return int((d + time.Second - 1) / time.Second) }

// POST /v1/device/code {client?, host?, os?, version?}
func (s *Server) deviceCode(w http.ResponseWriter, r *http.Request) {
	if limited(w, r, s.deviceLimit, "code:"+s.clientIP(r)) {
		return
	}
	var ci store.ClientInfo
	if err := decodeJSON(w, r, 4<<10, &ci); err != nil {
		failBody(w, r, err)
		return
	}
	ci = store.ClientInfo{Client: cleanClientField(ci.Client), Host: cleanClientField(ci.Host), OS: cleanClientField(ci.OS), Version: cleanClientField(ci.Version)}
	now := s.now()
	deviceCode := randomToken("")
	for range 5 {
		code := newUserCode()
		err := s.st.CreateDeviceAuth(r.Context(), store.DeviceAuth{
			DeviceCodeHash: hashSecret(deviceCode), UserCode: code, Client: ci, Status: store.DevicePending,
			Interval: s.opt.DevicePollInterval, CreatedAt: now, ExpiresAt: now.Add(s.opt.DeviceTTL),
		})
		if errors.Is(err, store.ErrConflict) {
			continue
		}
		if err != nil {
			s.internal(w, r, err)
			return
		}
		verify := s.cfg.DashboardURL + "/device"
		writeJSON(w, http.StatusOK, deviceCodeResponse{
			DeviceCode: deviceCode, UserCode: displayUserCode(code),
			VerificationURI: verify, VerificationURIComplete: verify + "?code=" + url.QueryEscape(displayUserCode(code)),
			ExpiresIn: int(s.opt.DeviceTTL.Seconds()), Interval: ceilSeconds(s.opt.DevicePollInterval),
		})
		return
	}
	s.internal(w, r, errors.New("could not allocate a unique user code"))
}

// POST /v1/device/token {device_code, grant_type?}
func (s *Server) deviceToken(w http.ResponseWriter, r *http.Request) {
	if limited(w, r, s.deviceLimit, "token:"+s.clientIP(r)) {
		return
	}
	var body struct {
		DeviceCode string `json:"device_code"`
		GrantType  string `json:"grant_type"`
	}
	if err := decodeJSON(w, r, 4<<10, &body); err != nil {
		failBody(w, r, err)
		return
	}
	if body.GrantType != "" && body.GrantType != deviceGrantType {
		fail(w, r, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be "+deviceGrantType)
		return
	}
	if body.DeviceCode == "" {
		fail(w, r, http.StatusBadRequest, string(store.PollExpired), "device code is unknown or expired; start again")
		return
	}
	raw := randomToken(tokenPrefix)
	dev, err := s.st.PollDeviceAuth(r.Context(), hashSecret(body.DeviceCode), s.now(), 5*time.Second,
		store.Token{ID: store.NewID("tok_"), Hash: hashSecret(raw), CreatedAt: s.now()})
	if errors.Is(err, store.ErrNotFound) {
		fail(w, r, http.StatusBadRequest, string(store.PollExpired), "device code is unknown or expired; start again")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	switch dev.Status {
	case store.PollApproved:
		u, err := s.st.GetUser(r.Context(), dev.UserID)
		if err != nil {
			s.internal(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, tokenResponse{AccessToken: raw, TokenType: "bearer", Login: u.Login})
	case store.PollSlowDown:
		writeJSON(w, http.StatusBadRequest, apiError{Error: string(dev.Status), Interval: ceilSeconds(dev.Interval), RequestID: requestID(r.Context())})
	case store.PollPending, store.PollDenied, store.PollExpired:
		fail(w, r, http.StatusBadRequest, string(dev.Status), "")
	}
}

type deviceInfo struct {
	UserCode  string           `json:"user_code"`
	Client    store.ClientInfo `json:"client"`
	ExpiresAt time.Time        `json:"expires_at"`
}

// GET /v1/device?user_code=XXXX-XXXX: what the approval page shows.
func (s *Server) deviceLookup(w http.ResponseWriter, r *http.Request, p *principal) {
	if limited(w, r, s.sessionLimit, "device:"+p.user.ID) {
		return
	}
	code, ok := normalizeUserCode(r.URL.Query().Get("user_code"))
	if !ok {
		fail(w, r, http.StatusNotFound, "invalid_user_code", "unknown or expired code")
		return
	}
	d, err := s.st.DeviceAuthByUserCode(r.Context(), code, s.now())
	if errors.Is(err, store.ErrNotFound) {
		fail(w, r, http.StatusNotFound, "invalid_user_code", "unknown or expired code")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, deviceInfo{UserCode: displayUserCode(d.UserCode), Client: d.Client, ExpiresAt: d.ExpiresAt.UTC()})
}

// POST /v1/device/approve {user_code, deny?}
func (s *Server) deviceApprove(w http.ResponseWriter, r *http.Request, p *principal) {
	if limited(w, r, s.sessionLimit, "device:"+p.user.ID) {
		return
	}
	var body struct {
		UserCode string `json:"user_code"`
		Deny     bool   `json:"deny"`
	}
	if err := decodeJSON(w, r, 4<<10, &body); err != nil {
		failBody(w, r, err)
		return
	}
	code, ok := normalizeUserCode(body.UserCode)
	if !ok {
		fail(w, r, http.StatusNotFound, "invalid_user_code", "unknown or expired code")
		return
	}
	err := s.st.DecideDeviceAuth(r.Context(), code, p.user.ID, !body.Deny, s.now())
	if errors.Is(err, store.ErrNotFound) {
		fail(w, r, http.StatusNotFound, "invalid_user_code", "unknown or expired code")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	status := "approved"
	if body.Deny {
		status = "denied"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}
