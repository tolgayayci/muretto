// Package memstore is an in-memory store.Store for tests and local development.
// Data is lost on restart.
package memstore

import (
	"context"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/tolgayayci/muretto/server/internal/store"
)

type token struct {
	store.Token
	revoked bool
}

type membership struct {
	role     store.Role
	joinedAt time.Time
}

type invite struct {
	store.Invite
	used bool
}

type recKey struct {
	userID string
	kind   store.Kind
	key    string
}

// Store is an in-memory store.Store.
type Store struct {
	mu          sync.Mutex
	users       map[string]store.User // by id
	identities  map[string]string     // identity -> user id
	tokens      map[string]*token     // by hash
	devices     map[string]*store.DeviceAuth
	orgs        map[string]store.Org
	memberships map[string]map[string]membership // org id -> user id -> membership
	invites     map[string]*invite
	records     map[recKey]store.Record
	events      []store.Event
	seq         int64
	requests    []store.ProviderRequest
	decisions   map[[2]string]store.ReviewDecision // (user id, resource id)
}

var _ store.Store = (*Store)(nil)

// maxEvents bounds the replay log; older events are dropped.
const maxEvents = 50000

// New returns an empty store.
func New() *Store {
	return &Store{
		users:       map[string]store.User{},
		identities:  map[string]string{},
		tokens:      map[string]*token{},
		devices:     map[string]*store.DeviceAuth{},
		orgs:        map[string]store.Org{},
		memberships: map[string]map[string]membership{},
		invites:     map[string]*invite{},
		records:     map[recKey]store.Record{},
		decisions:   map[[2]string]store.ReviewDecision{},
	}
}

// Close is a no-op.
func (s *Store) Close() {}

// ProviderRequests returns the stored provider requests (tests).
func (s *Store) ProviderRequests() []store.ProviderRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]store.ProviderRequest(nil), s.requests...)
}

func (s *Store) UpsertUser(_ context.Context, identity, login, displayName string, now time.Time) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.identities[identity]; ok {
		u := s.users[id]
		u.Login, u.DisplayName = login, displayName
		s.users[id] = u
		return u, nil
	}
	u := store.User{ID: store.NewID("usr_"), Identity: identity, Login: login, DisplayName: displayName, CreatedAt: now}
	s.users[u.ID] = u
	s.identities[identity] = u.ID
	return u, nil
}

func (s *Store) GetUser(_ context.Context, id string) (store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return u, nil
}

func (s *Store) CreateToken(_ context.Context, t store.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createTokenLocked(t)
}

func (s *Store) createTokenLocked(t store.Token) error {
	if _, ok := s.tokens[t.Hash]; ok {
		return store.ErrConflict
	}
	s.tokens[t.Hash] = &token{Token: t}
	return nil
}

func (s *Store) TokenUser(_ context.Context, hash string, now time.Time) (store.Token, store.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[hash]
	if !ok || t.revoked {
		return store.Token{}, store.User{}, store.ErrNotFound
	}
	u, ok := s.users[t.UserID]
	if !ok {
		return store.Token{}, store.User{}, store.ErrNotFound
	}
	if t.LastUsedAt.IsZero() || now.Sub(t.LastUsedAt) >= store.TokenTouchInterval {
		t.LastUsedAt = now
	}
	return t.Token, u, nil
}

func (s *Store) ListTokens(_ context.Context, userID string) ([]store.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []store.Token{}
	for _, t := range s.tokens {
		if t.UserID == userID && !t.revoked {
			out = append(out, t.Token)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

func (s *Store) RevokeUserToken(_ context.Context, userID, tokenID string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tokens {
		if t.ID == tokenID && t.UserID == userID && !t.revoked {
			t.revoked = true
			return nil
		}
	}
	return store.ErrNotFound
}

func (s *Store) RevokeToken(_ context.Context, hash string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tokens[hash]
	if !ok || t.revoked {
		return store.ErrNotFound
	}
	t.revoked = true
	return nil
}

func (s *Store) CreateDeviceAuth(_ context.Context, d store.DeviceAuth) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for h, other := range s.devices {
		if !d.CreatedAt.Before(other.ExpiresAt.Add(24 * time.Hour)) {
			delete(s.devices, h) // long expired
			continue
		}
		if other.UserCode == d.UserCode {
			return store.ErrConflict
		}
	}
	if _, ok := s.devices[d.DeviceCodeHash]; ok {
		return store.ErrConflict
	}
	cp := d
	s.devices[d.DeviceCodeHash] = &cp
	return nil
}

func (s *Store) pendingByUserCode(userCode string, now time.Time) *store.DeviceAuth {
	for _, d := range s.devices {
		if d.UserCode == userCode && d.Status == store.DevicePending && now.Before(d.ExpiresAt) {
			return d
		}
	}
	return nil
}

func (s *Store) DeviceAuthByUserCode(_ context.Context, userCode string, now time.Time) (store.DeviceAuth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.pendingByUserCode(userCode, now)
	if d == nil {
		return store.DeviceAuth{}, store.ErrNotFound
	}
	return *d, nil
}

func (s *Store) DecideDeviceAuth(_ context.Context, userCode, userID string, approve bool, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.pendingByUserCode(userCode, now)
	if d == nil {
		return store.ErrNotFound
	}
	d.UserID = userID
	if approve {
		d.Status = store.DeviceApproved
	} else {
		d.Status = store.DeviceDenied
	}
	return nil
}

func (s *Store) PollDeviceAuth(_ context.Context, deviceCodeHash string, now time.Time, slowDown time.Duration, mint store.Token) (store.PollResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[deviceCodeHash]
	if !ok {
		return store.PollResult{}, store.ErrNotFound
	}
	if !now.Before(d.ExpiresAt) || d.Status == store.DeviceConsumed {
		return store.PollResult{Status: store.PollExpired, Interval: d.Interval}, nil
	}
	if !d.LastPollAt.IsZero() && now.Sub(d.LastPollAt) < d.Interval {
		d.Interval += slowDown
		d.LastPollAt = now
		return store.PollResult{Status: store.PollSlowDown, Interval: d.Interval}, nil
	}
	d.LastPollAt = now
	switch d.Status {
	case store.DevicePending:
		return store.PollResult{Status: store.PollPending, Interval: d.Interval}, nil
	case store.DeviceDenied:
		d.Status = store.DeviceConsumed
		return store.PollResult{Status: store.PollDenied, Interval: d.Interval}, nil
	case store.DeviceApproved:
		mint.UserID = d.UserID
		if mint.Name == "" {
			mint.Name = d.Client.Host
		}
		if err := s.createTokenLocked(mint); err != nil {
			return store.PollResult{}, err
		}
		d.Status = store.DeviceConsumed
		return store.PollResult{Status: store.PollApproved, Interval: d.Interval, UserID: d.UserID}, nil
	case store.DeviceConsumed:
	}
	return store.PollResult{Status: store.PollExpired, Interval: d.Interval}, nil
}

func (s *Store) CreateOrg(_ context.Context, o store.Org, ownerID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[o.ID]; ok {
		return store.ErrConflict
	}
	s.orgs[o.ID] = o
	s.memberships[o.ID] = map[string]membership{ownerID: {role: store.RoleOwner, joinedAt: now}}
	return nil
}

func (s *Store) UserOrgs(_ context.Context, userID string) ([]store.Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Membership
	for orgID, ms := range s.memberships {
		if m, ok := ms[userID]; ok {
			out = append(out, store.Membership{Org: s.orgs[orgID], Role: m.role, JoinedAt: m.joinedAt})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Org.CreatedAt.Before(out[j].Org.CreatedAt) || (out[i].Org.CreatedAt.Equal(out[j].Org.CreatedAt) && out[i].Org.ID < out[j].Org.ID)
	})
	return out, nil
}

func (s *Store) Membership(_ context.Context, orgID, userID string) (store.Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.memberships[orgID][userID]
	if !ok {
		return store.Membership{}, store.ErrNotFound
	}
	return store.Membership{Org: s.orgs[orgID], Role: m.role, JoinedAt: m.joinedAt}, nil
}

func (s *Store) OrgMembers(_ context.Context, orgID string) ([]store.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orgs[orgID]; !ok {
		return nil, store.ErrNotFound
	}
	var out []store.Member
	for uid, m := range s.memberships[orgID] {
		out = append(out, store.Member{User: s.users[uid], Role: m.role, JoinedAt: m.joinedAt})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].JoinedAt.Before(out[j].JoinedAt) || (out[i].JoinedAt.Equal(out[j].JoinedAt) && out[i].User.ID < out[j].User.ID)
	})
	return out, nil
}

func (s *Store) CreateInvite(_ context.Context, inv store.Invite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.invites[inv.Hash]; ok {
		return store.ErrConflict
	}
	s.invites[inv.Hash] = &invite{Invite: inv}
	return nil
}

func (s *Store) AcceptInvite(_ context.Context, hash, userID string, now time.Time) (store.Membership, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.invites[hash]
	if !ok || inv.used || !now.Before(inv.ExpiresAt) {
		return store.Membership{}, store.ErrNotFound
	}
	org := s.orgs[inv.OrgID]
	if m, ok := s.memberships[inv.OrgID][userID]; ok {
		return store.Membership{Org: org, Role: m.role, JoinedAt: m.joinedAt}, nil
	}
	inv.used = true
	s.memberships[inv.OrgID][userID] = membership{role: store.RoleMember, joinedAt: now}
	return store.Membership{Org: org, Role: store.RoleMember, JoinedAt: now}, nil
}

func (s *Store) Ingest(_ context.Context, in store.IngestInput) (store.IngestResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var res store.IngestResult
	var accepted []store.Record
	for _, r := range in.Records {
		k := recKey{in.UserID, r.Kind, r.Key}
		if old, ok := s.records[k]; ok && old.Rev == r.Rev {
			res.Unchanged++
			continue
		}
		r.UserID = in.UserID
		r.UpdatedAt = in.Now
		s.records[k] = r
		accepted = append(accepted, r)
		res.Accepted++
	}
	for _, e := range store.EventsFor(accepted, in) {
		s.seq++
		e.Seq = s.seq
		s.events = append(s.events, e)
		res.Events = append(res.Events, e)
	}
	if len(s.events) > maxEvents {
		s.events = slices.Clone(s.events[len(s.events)-maxEvents/2:])
	}
	return res, nil
}

func (s *Store) Records(_ context.Context, userIDs []string) ([]store.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Record
	for k, r := range s.records {
		if slices.Contains(userIDs, k.userID) {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.UserID != b.UserID {
			return a.UserID < b.UserID
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Key < b.Key
	})
	return out, nil
}

func (s *Store) EventsAfter(_ context.Context, userIDs []string, after int64, limit int) ([]store.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Event
	for _, e := range s.events {
		if e.Seq <= after || (userIDs != nil && !slices.Contains(userIDs, e.UserID)) {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *Store) SetReviewDecision(_ context.Context, d store.ReviewDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[d.UserID]; !ok {
		return store.ErrNotFound
	}
	k := [2]string{d.UserID, d.ResourceID}
	if old, ok := s.decisions[k]; ok && old.DecidedAt.After(d.DecidedAt) {
		return nil
	}
	s.decisions[k] = d
	return nil
}

func (s *Store) ReviewDecisions(_ context.Context, userIDs []string) ([]store.ReviewDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []store.ReviewDecision{}
	for _, d := range s.decisions {
		if slices.Contains(userIDs, d.UserID) {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UserID != out[j].UserID {
			return out[i].UserID < out[j].UserID
		}
		return out[i].ResourceID < out[j].ResourceID
	})
	return out, nil
}

func (s *Store) AddProviderRequest(_ context.Context, req store.ProviderRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, req)
	return nil
}
