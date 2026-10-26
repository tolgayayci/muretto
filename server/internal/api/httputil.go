package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

// apiError is every error body: {"error": "<code>", "message": "...", "request_id": "..."}.
type apiError struct {
	Error     string   `json:"error"`
	Message   string   `json:"message,omitempty"`
	Problems  []string `json:"problems,omitempty"`
	Interval  int      `json:"interval,omitempty"`
	RequestID string   `json:"request_id,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: code, Message: msg, RequestID: requestID(r.Context())})
}

var (
	errTooLarge    = errors.New("body too large")
	errContentType = errors.New("content type must be application/json")
)

// decodeJSON strictly decodes exactly one JSON value: application/json only,
// size-capped, unknown fields rejected, trailing data rejected.
func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, dst any) error {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return errContentType
	}
	body := http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return wrapBodyErr(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err != nil {
			if e := wrapBodyErr(err); errors.Is(e, errTooLarge) {
				return e
			}
		}
		return errors.New("body must contain a single JSON value")
	}
	return nil
}

// readBody reads a capped JSON body as bytes.
func readBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return nil, errContentType
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		return nil, wrapBodyErr(err)
	}
	return b, nil
}

func wrapBodyErr(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return errTooLarge
	}
	return err
}

// failBody maps a decode error to a response.
func failBody(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errTooLarge):
		fail(w, r, http.StatusRequestEntityTooLarge, "body_too_large", "request body is too large")
	case errors.Is(err, errContentType):
		fail(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", err.Error())
	default:
		fail(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON for this endpoint")
	}
}

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// randomToken returns prefix + 32 random bytes in lower-case base32.
func randomToken(prefix string) string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return prefix + toLower(b32.EncodeToString(b))
}

func toLower(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + 32
		}
	}
	return string(out)
}

// hashSecret is how every secret (token, session, device code, invite) is stored.
func hashSecret(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

type ctxKey int

const requestIDKey ctxKey = iota

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}
