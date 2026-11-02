// Package store defines the sync server's storage interface and the types it
// persists. memstore implements it in memory (tests, local dev); pgstore
// implements it on Postgres.
//
// Secrets are never stored: CLI tokens, device codes and invite tokens are
// kept only as sha256 hashes. Dashboard users authenticate with Supabase Auth
// access tokens, which the API verifies and never stores.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Errors returned by every implementation.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

// User is a person, identified by an external identity ("supabase:<user
// uuid>", or "dev:<login>" with the dev-only login bypass). Older databases
// may also hold "github:<numeric id>" users from the retired GitHub OAuth
// login.
type User struct {
	ID          string
	Identity    string
	Login       string
	DisplayName string
	CreatedAt   time.Time
}

// Name is what other people see: the display name, or the login.
func (u User) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Login
}

// Token is a CLI API token. Only its sha256 hash is stored.
type Token struct {
	ID         string
	Hash       string
	UserID     string
	Name       string // the CLI host name, for the "machines" list
	CreatedAt  time.Time
	LastUsedAt time.Time // zero: never used
}

// TokenTouchInterval is how stale last_used_at may get: TokenUser writes it
// at most once per interval per token.
const TokenTouchInterval = time.Minute

// ClientInfo describes the CLI asking to log in; shown on the approval page.
type ClientInfo struct {
	Client  string `json:"client,omitempty"`
	Host    string `json:"host,omitempty"`
	OS      string `json:"os,omitempty"`
	Version string `json:"version,omitempty"`
}

// DeviceStatus is the lifecycle of a device authorization.
type DeviceStatus string

const (
	DevicePending  DeviceStatus = "pending"
	DeviceApproved DeviceStatus = "approved"
	DeviceDenied   DeviceStatus = "denied"
	DeviceConsumed DeviceStatus = "consumed"
)

// DeviceAuth is one CLI login attempt (RFC 8628 shaped).
type DeviceAuth struct {
	DeviceCodeHash string
	UserCode       string // normalized: 8 upper-case letters, no hyphen
	Client         ClientInfo
	Status         DeviceStatus
	UserID         string
	Interval       time.Duration
	CreatedAt      time.Time
	ExpiresAt      time.Time
	LastPollAt     time.Time
}

// PollStatus is the outcome of one device token poll.
type PollStatus string

const (
	PollPending  PollStatus = "authorization_pending"
	PollSlowDown PollStatus = "slow_down"
	PollApproved PollStatus = "approved"
	PollDenied   PollStatus = "access_denied"
	PollExpired  PollStatus = "expired_token"
)

// PollResult is returned by PollDeviceAuth.
type PollResult struct {
	Status   PollStatus
	Interval time.Duration
	UserID   string
}

// Role is an org role.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleMember Role = "member"
)

// Org is a team.
type Org struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// Membership is one of a user's orgs.
type Membership struct {
	Org      Org
	Role     Role
	JoinedAt time.Time
}

// Member is one user in an org.
type Member struct {
	User     User
	Role     Role
	JoinedAt time.Time
}

// Invite is a single-use, expiring org invite. Only the token hash is stored.
type Invite struct {
	Hash      string
	OrgID     string
	CreatedBy string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// ReviewDecision is a "still need this?" decision made in the dashboard, one
// per (user, resource). State is "keep", "not_needed" or "pending" (an undo).
// DecidedAt is the server time of the decision; an undo keeps its own time so
// "latest wins" against CLI decisions still works. Note is empty when unset.
type ReviewDecision struct {
	UserID     string
	ResourceID string
	State      string
	Note       string
	DecidedAt  time.Time
}

// Kind is the entity kind of a synced record. It doubles as the SSE event type.
type Kind string

const (
	KindSession     Kind = "session"
	KindResource    Kind = "resource"
	KindAttribution Kind = "attribution"
	KindFinding     Kind = "finding"
	KindCost        Kind = "cost"
	KindReview      Kind = "review"
	KindCheck       Kind = "check"
)

// Record is one synced entity owned by one user, upserted by (UserID, Kind, Key).
type Record struct {
	UserID    string
	Kind      Kind
	Key       string
	Rev       string
	Data      json.RawMessage
	UpdatedAt time.Time
}

// Event is one live-update notification. It carries ids only: the dashboard
// re-fetches data through the read API, which does its own authorization.
type Event struct {
	Seq       int64     `json:"seq"`
	UserID    string    `json:"-"`
	Type      string    `json:"type"`
	EntityID  string    `json:"id"`
	Developer string    `json:"developer"`
	At        time.Time `json:"at"`
}

// EventBulk is the event type sent instead of per-entity events when one batch
// changes more than MaxEventsPerBatch entities. Its id is the batch id.
const EventBulk = "bulk"

// MaxEventsPerBatch caps per-entity events per ingest.
const MaxEventsPerBatch = 100

// IngestInput is one validated sync batch.
type IngestInput struct {
	UserID    string
	Developer string // the user's login, copied onto events
	BatchID   string
	Records   []Record
	Now       time.Time
}

// IngestResult counts what changed and lists the events written.
type IngestResult struct {
	Accepted  int
	Unchanged int
	Events    []Event
}

// Store is the server's persistence. Every method is safe for concurrent use.
type Store interface {
	UpsertUser(ctx context.Context, identity, login, displayName string, now time.Time) (User, error)
	GetUser(ctx context.Context, id string) (User, error)

	CreateToken(ctx context.Context, t Token) error
	// TokenUser returns the token and its user; ErrNotFound if unknown or
	// revoked. It sets last_used_at to now when it is older than
	// TokenTouchInterval (or unset), and the returned token reflects that.
	TokenUser(ctx context.Context, hash string, now time.Time) (Token, User, error)
	// RevokeToken revokes by hash; ErrNotFound if unknown or already revoked.
	RevokeToken(ctx context.Context, hash string, now time.Time) error
	// ListTokens returns the user's unrevoked tokens, newest first.
	ListTokens(ctx context.Context, userID string) ([]Token, error)
	// RevokeUserToken revokes token id only if it belongs to userID and is not
	// revoked yet; ErrNotFound otherwise (callers must not tell the cases apart).
	RevokeUserToken(ctx context.Context, userID, tokenID string, now time.Time) error

	// CreateDeviceAuth returns ErrConflict if the user code is in use.
	CreateDeviceAuth(ctx context.Context, d DeviceAuth) error
	// DeviceAuthByUserCode returns a pending, unexpired authorization.
	DeviceAuthByUserCode(ctx context.Context, userCode string, now time.Time) (DeviceAuth, error)
	// DecideDeviceAuth approves or denies a pending, unexpired authorization.
	DecideDeviceAuth(ctx context.Context, userCode, userID string, approve bool, now time.Time) error
	// PollDeviceAuth applies one poll atomically: slow_down when polled faster
	// than the interval (which then grows by slowDown), and on approval marks
	// the authorization consumed and stores mint (with UserID set, and Name
	// set to the client's host when empty) in the same step, so a device code
	// yields at most one token. ErrNotFound if unknown.
	PollDeviceAuth(ctx context.Context, deviceCodeHash string, now time.Time, slowDown time.Duration, mint Token) (PollResult, error)

	CreateOrg(ctx context.Context, o Org, ownerID string, now time.Time) error
	UserOrgs(ctx context.Context, userID string) ([]Membership, error)
	Membership(ctx context.Context, orgID, userID string) (Membership, error)
	OrgMembers(ctx context.Context, orgID string) ([]Member, error)
	CreateInvite(ctx context.Context, inv Invite) error
	// AcceptInvite consumes an unused, unexpired invite and adds the user. A user
	// who is already a member gets their membership back and the invite stays
	// unused. ErrNotFound for unknown, used or expired invites.
	AcceptInvite(ctx context.Context, hash, userID string, now time.Time) (Membership, error)

	// Ingest upserts records whose rev changed and appends their events.
	Ingest(ctx context.Context, in IngestInput) (IngestResult, error)
	// Records returns every record owned by the given users.
	Records(ctx context.Context, userIDs []string) ([]Record, error)
	// EventsAfter returns events with Seq > after, oldest first. A nil userIDs
	// means every user.
	EventsAfter(ctx context.Context, userIDs []string, after int64, limit int) ([]Event, error)

	// SetReviewDecision upserts the user's dashboard decision for a resource.
	// An existing decision with a later DecidedAt is kept.
	SetReviewDecision(ctx context.Context, d ReviewDecision) error
	// ReviewDecisions returns the decisions of the given users, ordered by
	// user id and resource id.
	ReviewDecisions(ctx context.Context, userIDs []string) ([]ReviewDecision, error)

	AddProviderRequest(ctx context.Context, req ProviderRequest) error

	Close()
}

// EventsFor builds the events for the records an ingest accepted. Seq is left
// for the store to assign.
func EventsFor(accepted []Record, in IngestInput) []Event {
	if len(accepted) == 0 {
		return nil
	}
	if len(accepted) > MaxEventsPerBatch {
		return []Event{{UserID: in.UserID, Type: EventBulk, EntityID: in.BatchID, Developer: in.Developer, At: in.Now}}
	}
	evs := make([]Event, 0, len(accepted))
	for _, r := range accepted {
		evs = append(evs, Event{UserID: in.UserID, Type: string(r.Kind), EntityID: r.Key, Developer: in.Developer, At: in.Now})
	}
	return evs
}

// ProviderRequest is one "read this provider next" request: from the landing
// page (maybe with an email) or from a signed-in dashboard user.
type ProviderRequest struct {
	Provider string
	Email    string // optional
	UserID   string // set when a signed-in user asked
	At       time.Time
}
