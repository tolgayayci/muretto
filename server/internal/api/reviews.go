package api

import (
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tolgayayci/muretto/server/internal/store"
)

// resourceIDPattern is a Muretto resource id, "<provider>.<kind>:<provider_id>"
// (e.g. supabase.project:abcd, vercel.env_var:prj_x/KEY). The provider id may
// be any printable text without whitespace; isPrintableID checks that.
var resourceIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_.]*:\S+$`)

const (
	maxResourceIDLen = 512
	maxReviewNote    = 500 // runes
)

func isPrintableID(s string) bool {
	for _, r := range s {
		if !unicode.IsPrint(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// validNote allows printable text and line breaks.
func validNote(s string) bool {
	if utf8.RuneCountInString(s) > maxReviewNote {
		return false
	}
	for _, r := range s {
		if r != '\n' && !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// POST /v1/reviews {resource_id, state, note?}: a Keep / Not needed / undo
// ("pending") decision from the dashboard. Stored per user; the answer views
// merge it with the CLI's synced review (latest wins, see view/decisions.go).
// A decision for a resource the user has no synced review for is stored and
// has no effect.
func (s *Server) setReview(w http.ResponseWriter, r *http.Request, p *principal) {
	if limited(w, r, s.reviewLimit, p.user.ID) {
		return
	}
	var body struct {
		ResourceID string  `json:"resource_id"`
		State      string  `json:"state"`
		Note       *string `json:"note"`
	}
	if err := decodeJSON(w, r, 4<<10, &body); err != nil {
		failBody(w, r, err)
		return
	}
	if len(body.ResourceID) > maxResourceIDLen || !utf8.ValidString(body.ResourceID) ||
		!resourceIDPattern.MatchString(body.ResourceID) || !isPrintableID(body.ResourceID) {
		fail(w, r, http.StatusBadRequest, "invalid_resource_id", "resource_id must be a Muretto resource id like supabase.project:<ref>, at most 512 characters")
		return
	}
	switch body.State {
	case "keep", "not_needed", "pending":
	default:
		fail(w, r, http.StatusBadRequest, "invalid_state", "state must be keep, not_needed or pending")
		return
	}
	note := ""
	if body.Note != nil {
		note = strings.TrimSpace(strings.ReplaceAll(*body.Note, "\r\n", "\n"))
		if !utf8.ValidString(note) || !validNote(note) {
			fail(w, r, http.StatusBadRequest, "invalid_note", "note must be at most 500 printable characters")
			return
		}
	}
	err := s.st.SetReviewDecision(r.Context(), store.ReviewDecision{
		UserID: p.user.ID, ResourceID: body.ResourceID, State: body.State, Note: note, DecidedAt: s.now().UTC(),
	})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type reviewDecision struct {
	ResourceID string    `json:"resource_id"`
	State      string    `json:"state"`
	Note       *string   `json:"note,omitempty"`
	DecidedAt  time.Time `json:"decided_at"`
}

// GET /v1/reviews: the caller's own dashboard decisions (the CLI pulls them).
func (s *Server) listReviews(w http.ResponseWriter, r *http.Request, p *principal) {
	ds, err := s.st.ReviewDecisions(r.Context(), []string{p.user.ID})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	out := make([]reviewDecision, 0, len(ds))
	for _, d := range ds {
		rd := reviewDecision{ResourceID: d.ResourceID, State: d.State, DecidedAt: d.DecidedAt.UTC().Truncate(time.Second)}
		if d.Note != "" {
			note := d.Note
			rd.Note = &note
		}
		out = append(out, rd)
	}
	writeJSON(w, http.StatusOK, map[string]any{"decisions": out})
}
