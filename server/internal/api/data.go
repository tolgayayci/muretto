package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/server/internal/store"
	"github.com/tolgayayci/muretto/server/internal/validate"
	"github.com/tolgayayci/muretto/server/internal/view"
)

// POST /v1/ingest: body is a SyncBatch, validated against
// schema/sync-batch.schema.json. Data belongs to the authenticated user.
func (s *Server) ingest(w http.ResponseWriter, r *http.Request, p *principal) {
	if limited(w, r, s.ingestLimit, p.tokenHash) {
		return
	}
	raw, err := readBody(w, r, s.cfg.MaxBody)
	if err != nil {
		failBody(w, r, err)
		return
	}
	if err := s.validator.SyncBatch(raw); err != nil {
		var ve *validate.Error
		if errors.As(err, &ve) {
			writeJSON(w, http.StatusUnprocessableEntity, apiError{Error: "invalid_batch", Message: "body does not match sync-batch.schema.json",
				Problems: ve.Problems, RequestID: requestID(r.Context())})
			return
		}
		s.internal(w, r, err)
		return
	}
	var b contract.SyncBatch
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		fail(w, r, http.StatusBadRequest, "invalid_json", "body could not be decoded")
		return
	}
	recs, err := batchRecords(b)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	res, err := s.st.Ingest(r.Context(), store.IngestInput{
		UserID: p.user.ID, Developer: p.user.Login, BatchID: b.BatchID, Records: recs, Now: s.now().UTC(),
	})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if s.opt.PublishLocal && len(res.Events) > 0 {
		s.hub.Publish(res.Events)
	}
	writeJSON(w, http.StatusOK, map[string]int{"accepted": res.Accepted, "unchanged": res.Unchanged})
}

// batchRecords flattens a batch into keyed records. A key repeated within one
// batch keeps its last occurrence.
func batchRecords(b contract.SyncBatch) ([]store.Record, error) {
	var out []store.Record
	idx := map[string]int{}
	add := func(kind store.Kind, key string, rev contract.Rev, entity any) error {
		data, err := json.Marshal(entity)
		if err != nil {
			return err
		}
		rec := store.Record{Kind: kind, Key: key, Rev: string(rev), Data: data}
		k := string(kind) + "\x00" + key
		if i, ok := idx[k]; ok {
			out[i] = rec
			return nil
		}
		idx[k] = len(out)
		out = append(out, rec)
		return nil
	}
	var errs []error
	for _, x := range b.Sessions {
		errs = append(errs, add(store.KindSession, x.Entity.ID, x.Rev, x.Entity))
	}
	for _, x := range b.Resources {
		errs = append(errs, add(store.KindResource, x.Entity.ID, x.Rev, x.Entity))
	}
	for _, x := range b.Attributions {
		errs = append(errs, add(store.KindAttribution, x.Entity.ResourceID, x.Rev, x.Entity))
	}
	for _, x := range b.Findings {
		errs = append(errs, add(store.KindFinding, x.Entity.ID, x.Rev, x.Entity))
	}
	for _, x := range b.Costs {
		errs = append(errs, add(store.KindCost, store.CostKey(x.Entity), x.Rev, x.Entity))
	}
	for _, x := range b.Reviews {
		errs = append(errs, add(store.KindReview, x.Entity.ResourceID, x.Rev, x.Entity))
	}
	for _, x := range b.Checks {
		errs = append(errs, add(store.KindCheck, store.CheckKey(x.Entity), x.Rev, x.Entity))
	}
	return out, errors.Join(errs...)
}

// GET /v1/answer?scope=me
func (s *Server) answerMe(w http.ResponseWriter, r *http.Request, p *principal) {
	if sc := r.URL.Query().Get("scope"); sc != "" && sc != "me" {
		fail(w, r, http.StatusBadRequest, "invalid_scope", "scope must be me; the team view is GET /v1/orgs/{id}/answer")
		return
	}
	recs, err := s.st.Records(r.Context(), []string{p.user.ID})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.writeAnswer(w, r, recs, []string{p.user.ID}, map[string]string{p.user.ID: p.user.Name()}, p.user)
}

// GET /v1/orgs/{id}/answer: the union of the members' data.
func (s *Server) answerTeam(w http.ResponseWriter, r *http.Request, p *principal) {
	m, ok := s.member(w, r, p)
	if !ok {
		return
	}
	members, err := s.st.OrgMembers(r.Context(), m.Org.ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	ids := make([]string, 0, len(members))
	names := map[string]string{}
	for _, mm := range members {
		ids = append(ids, mm.User.ID)
		names[mm.User.ID] = mm.User.Name()
	}
	recs, err := s.st.Records(r.Context(), ids)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.writeAnswer(w, r, recs, ids, names, p.user)
}

// writeAnswer composes the Answer from the owners' records and their
// dashboard review decisions.
func (s *Server) writeAnswer(w http.ResponseWriter, r *http.Request, recs []store.Record, owners []string, names map[string]string, u store.User) {
	decisions, err := s.st.ReviewDecisions(r.Context(), owners)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	a, err := view.ComposeWithDecisions(recs, decisions, names, view.Viewer{UserID: u.ID, Login: u.Login, DisplayName: u.DisplayName}, s.now())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

var providerName = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ._+&/-]{0,63}$`)

// POST /v1/provider-requests {provider, email?}: "read this provider next".
// Anonymous from the landing page, limited per IP. With a dashboard sign-in
// it records who asked and is limited per user: every dashboard request comes
// from the dashboard server's one address.
func (s *Server) providerRequest(w http.ResponseWriter, r *http.Request) {
	if _, ok := bearer(r); ok {
		s.authed(authUser, func(w http.ResponseWriter, r *http.Request, p *principal) {
			s.saveProviderRequest(w, r, "user:"+p.user.ID, p.user.ID)
		})(w, r)
		return
	}
	s.saveProviderRequest(w, r, s.clientIP(r), "")
}

func (s *Server) saveProviderRequest(w http.ResponseWriter, r *http.Request, limitKey, userID string) {
	if limited(w, r, s.requestLimit, limitKey) {
		return
	}
	var body struct {
		Provider string `json:"provider"`
		Email    string `json:"email"`
	}
	if err := decodeJSON(w, r, 2<<10, &body); err != nil {
		failBody(w, r, err)
		return
	}
	provider := strings.TrimSpace(body.Provider)
	if !providerName.MatchString(provider) {
		fail(w, r, http.StatusBadRequest, "invalid_provider", "provider must be 1 to 64 letters, digits, spaces or . _ + & / -")
		return
	}
	email := strings.TrimSpace(body.Email)
	if email != "" {
		at := strings.IndexByte(email, '@')
		if len(email) > 254 || at < 1 || at != strings.LastIndexByte(email, '@') || at == len(email)-1 || strings.ContainsAny(email, " \t\r\n<>\"") {
			fail(w, r, http.StatusBadRequest, "invalid_email", "email is not valid")
			return
		}
	}
	req := store.ProviderRequest{Provider: provider, Email: email, UserID: userID, At: s.now().UTC().Truncate(time.Second)}
	if err := s.st.AddProviderRequest(r.Context(), req); err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "received"})
}
