package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"
	"github.com/tolgayayci/muretto/server/internal/store"
)

type orgRef struct {
	ID   string     `json:"id"`
	Name string     `json:"name"`
	Role store.Role `json:"role"`
}

type meResponse struct {
	Login       string   `json:"login"`
	DisplayName string   `json:"display_name"`
	Orgs        []orgRef `json:"orgs"`
}

// GET /v1/me
func (s *Server) me(w http.ResponseWriter, r *http.Request, p *principal) {
	ms, err := s.st.UserOrgs(r.Context(), p.user.ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	resp := meResponse{Login: p.user.Login, DisplayName: p.user.Name(), Orgs: []orgRef{}}
	for _, m := range ms {
		resp.Orgs = append(resp.Orgs, orgRef{ID: m.Org.ID, Name: m.Org.Name, Role: m.Role})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) orgContract(r *http.Request, o store.Org) (contract.Org, error) {
	members, err := s.st.OrgMembers(r.Context(), o.ID)
	if err != nil {
		return contract.Org{}, err
	}
	out := contract.Org{ID: o.ID, Name: o.Name, CreatedAt: o.CreatedAt.UTC(), Members: []contract.Member{}}
	for _, m := range members {
		name := m.User.Name()
		out.Members = append(out.Members, contract.Member{
			DeveloperID: m.User.ID, AccountLogin: m.User.Login, DisplayName: &name,
			Role: contract.MemberRole(m.Role), JoinedAt: m.JoinedAt.UTC(),
		})
	}
	return out, nil
}

// POST /v1/orgs {name}
func (s *Server) createOrg(w http.ResponseWriter, r *http.Request, p *principal) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, 4<<10, &body); err != nil {
		failBody(w, r, err)
		return
	}
	name := cleanClientField(body.Name)
	if name == "" {
		fail(w, r, http.StatusBadRequest, "invalid_name", "name must be 1 to 100 printable characters")
		return
	}
	now := s.now().UTC().Truncate(time.Second)
	o := store.Org{ID: store.NewID("org_"), Name: name, CreatedAt: now}
	if err := s.st.CreateOrg(r.Context(), o, p.user.ID, now); err != nil {
		s.internal(w, r, err)
		return
	}
	out, err := s.orgContract(r, o)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// member loads the caller's membership; non-members get 404 so org ids do not
// leak existence.
func (s *Server) member(w http.ResponseWriter, r *http.Request, p *principal) (store.Membership, bool) {
	m, err := s.st.Membership(r.Context(), r.PathValue("id"), p.user.ID)
	if errors.Is(err, store.ErrNotFound) {
		fail(w, r, http.StatusNotFound, "not_found", "org not found")
		return m, false
	}
	if err != nil {
		s.internal(w, r, err)
		return m, false
	}
	return m, true
}

// GET /v1/orgs/{id}
func (s *Server) getOrg(w http.ResponseWriter, r *http.Request, p *principal) {
	m, ok := s.member(w, r, p)
	if !ok {
		return
	}
	out, err := s.orgContract(r, m.Org)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /v1/orgs/{id}/invites (owner)
func (s *Server) createInvite(w http.ResponseWriter, r *http.Request, p *principal) {
	m, ok := s.member(w, r, p)
	if !ok {
		return
	}
	if m.Role != store.RoleOwner {
		fail(w, r, http.StatusForbidden, "forbidden", "only org owners can create invites")
		return
	}
	raw := randomToken(invitePrefix)
	now := s.now()
	inv := store.Invite{Hash: hashSecret(raw), OrgID: m.Org.ID, CreatedBy: p.user.ID, CreatedAt: now, ExpiresAt: now.Add(s.opt.InviteTTL)}
	if err := s.st.CreateInvite(r.Context(), inv); err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"url":        s.cfg.DashboardURL + "/join/" + raw,
		"expires_at": inv.ExpiresAt.UTC().Truncate(time.Second),
	})
}

// POST /v1/invites/{token}/accept
func (s *Server) acceptInvite(w http.ResponseWriter, r *http.Request, p *principal) {
	if limited(w, r, s.sessionLimit, "invite:"+p.user.ID) {
		return
	}
	raw := r.PathValue("token")
	if !strings.HasPrefix(raw, invitePrefix) || len(raw) > 100 {
		fail(w, r, http.StatusNotFound, "invalid_invite", "this invite is invalid, already used or expired")
		return
	}
	m, err := s.st.AcceptInvite(r.Context(), hashSecret(raw), p.user.ID, s.now())
	if errors.Is(err, store.ErrNotFound) {
		fail(w, r, http.StatusNotFound, "invalid_invite", "this invite is invalid, already used or expired")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, orgRef{ID: m.Org.ID, Name: m.Org.Name, Role: m.Role})
}
