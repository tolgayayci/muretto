// Package pgstore implements store.Store on Postgres with pgx.
//
// Production runs against Supabase through the Supavisor session-mode pooler
// (port 5432), which keeps prepared statements and LISTEN/NOTIFY working
// (decision 0503). The transaction-mode pooler (port 6543) is
// supported for queries only: statements then run in exec mode, and the
// LISTEN connection must use a session-mode or direct URL.
package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tolgayayci/muretto/server/internal/store"
)

// Channel is the NOTIFY channel carrying events between server instances.
const Channel = "muretto_events"

// Options configures the pool.
type Options struct {
	MaxConns int32
}

// Store is a Postgres-backed store.Store.
type Store struct {
	pool *pgxpool.Pool
}

var _ store.Store = (*Store)(nil)

// PoolConfig parses url and applies Muretto's pool settings.
func PoolConfig(url string, o Options) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("invalid database URL") // the parse error may echo the password
	}
	if o.MaxConns > 0 {
		cfg.MaxConns = o.MaxConns
	}
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = time.Minute
	cfg.ConnConfig.RuntimeParams["application_name"] = "murettod"
	if IsTransactionPooler(cfg.ConnConfig.Port) {
		// Transaction-mode poolers do not support prepared statements.
		cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	}
	return cfg, nil
}

// IsTransactionPooler reports whether port is Supavisor's transaction mode.
func IsTransactionPooler(port uint16) bool { return port == 6543 }

// Open connects, pings and runs migrations.
func Open(ctx context.Context, url string, o Options) (*Store, []string, error) {
	cfg, err := PoolConfig(url, o)
	if err != nil {
		return nil, nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("ping: %w", err)
	}
	applied, err := Migrate(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{pool: pool}, applied, nil
}

// Close closes the pool.
func (s *Store) Close() { s.pool.Close() }

// Pool exposes the pool (tests).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	return err
}

func isUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func scanUser(row pgx.Row) (store.User, error) {
	var u store.User
	err := row.Scan(&u.ID, &u.Identity, &u.Login, &u.DisplayName, &u.CreatedAt)
	return u, notFound(err)
}

const userCols = `u.id, u.identity, u.login, u.display_name, u.created_at`

func (s *Store) UpsertUser(ctx context.Context, identity, login, displayName string, now time.Time) (store.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		insert into users as u (id, identity, login, display_name, created_at) values ($1, $2, $3, $4, $5)
		on conflict (identity) do update set login = excluded.login, display_name = excluded.display_name
		returning `+userCols, store.NewID("usr_"), identity, login, displayName, now))
}

func (s *Store) GetUser(ctx context.Context, id string) (store.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `select `+userCols+` from users u where u.id = $1`, id))
}

func (s *Store) CreateToken(ctx context.Context, t store.Token) error {
	return createToken(ctx, s.pool, t)
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func createToken(ctx context.Context, db execer, t store.Token) error {
	_, err := db.Exec(ctx, `insert into api_tokens (id, token_hash, user_id, name, created_at) values ($1, $2, $3, $4, $5)`,
		t.ID, t.Hash, t.UserID, t.Name, t.CreatedAt)
	if isUniqueViolation(err) {
		return store.ErrConflict
	}
	return err
}

func (s *Store) TokenUser(ctx context.Context, hash string, now time.Time) (store.Token, store.User, error) {
	var t store.Token
	var u store.User
	var lastUsed *time.Time
	err := s.pool.QueryRow(ctx, `
		select t.id, t.token_hash, t.user_id, t.name, t.created_at, t.last_used_at, `+userCols+`
		from api_tokens t join users u on u.id = t.user_id
		where t.token_hash = $1 and t.revoked_at is null`, hash).
		Scan(&t.ID, &t.Hash, &t.UserID, &t.Name, &t.CreatedAt, &lastUsed, &u.ID, &u.Identity, &u.Login, &u.DisplayName, &u.CreatedAt)
	if err != nil {
		return store.Token{}, store.User{}, notFound(err)
	}
	if lastUsed != nil {
		t.LastUsedAt = *lastUsed
	}
	// Write last_used_at at most once per TokenTouchInterval, so every CLI
	// request is not also a row update.
	if lastUsed == nil || now.Sub(*lastUsed) >= store.TokenTouchInterval {
		if _, err := s.pool.Exec(ctx, `update api_tokens set last_used_at = $2
			where id = $1 and revoked_at is null and (last_used_at is null or last_used_at <= $3)`,
			t.ID, now, now.Add(-store.TokenTouchInterval)); err != nil {
			return store.Token{}, store.User{}, err
		}
		t.LastUsedAt = now
	}
	return t, u, nil
}

func (s *Store) RevokeToken(ctx context.Context, hash string, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `update api_tokens set revoked_at = $2 where token_hash = $1 and revoked_at is null`, hash, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) ListTokens(ctx context.Context, userID string) ([]store.Token, error) {
	rows, err := s.pool.Query(ctx, `select id, user_id, name, created_at, last_used_at from api_tokens
		where user_id = $1 and revoked_at is null order by created_at desc, id desc`, userID)
	if err != nil {
		return nil, err
	}
	out := []store.Token{}
	for rows.Next() {
		var t store.Token
		var lastUsed *time.Time
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &lastUsed); err != nil {
			rows.Close()
			return nil, err
		}
		if lastUsed != nil {
			t.LastUsedAt = *lastUsed
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) RevokeUserToken(ctx context.Context, userID, tokenID string, now time.Time) error {
	tag, err := s.pool.Exec(ctx, `update api_tokens set revoked_at = $3 where id = $1 and user_id = $2 and revoked_at is null`,
		tokenID, userID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) CreateDeviceAuth(ctx context.Context, d store.DeviceAuth) error {
	client, err := json.Marshal(d.Client)
	if err != nil {
		return err
	}
	_, _ = s.pool.Exec(ctx, `delete from device_auths where expires_at < $1`, d.CreatedAt.Add(-24*time.Hour))
	_, err = s.pool.Exec(ctx, `insert into device_auths
		(device_code_hash, user_code, client_info, status, interval_ms, created_at, expires_at)
		values ($1, $2, $3, $4, $5, $6, $7)`,
		d.DeviceCodeHash, d.UserCode, client, string(d.Status), d.Interval.Milliseconds(), d.CreatedAt, d.ExpiresAt)
	if isUniqueViolation(err) {
		return store.ErrConflict
	}
	return err
}

const deviceCols = `device_code_hash, user_code, client_info, status, coalesce(user_id, ''), interval_ms, created_at, expires_at, last_poll_at`

func scanDevice(row pgx.Row) (store.DeviceAuth, error) {
	var d store.DeviceAuth
	var client []byte
	var status string
	var intervalMS int64
	var lastPoll *time.Time
	if err := row.Scan(&d.DeviceCodeHash, &d.UserCode, &client, &status, &d.UserID, &intervalMS, &d.CreatedAt, &d.ExpiresAt, &lastPoll); err != nil {
		return d, notFound(err)
	}
	d.Status = store.DeviceStatus(status)
	d.Interval = time.Duration(intervalMS) * time.Millisecond
	if lastPoll != nil {
		d.LastPollAt = *lastPoll
	}
	if err := json.Unmarshal(client, &d.Client); err != nil {
		return d, err
	}
	return d, nil
}

func (s *Store) DeviceAuthByUserCode(ctx context.Context, userCode string, now time.Time) (store.DeviceAuth, error) {
	return scanDevice(s.pool.QueryRow(ctx, `select `+deviceCols+` from device_auths
		where user_code = $1 and status = 'pending' and expires_at > $2`, userCode, now))
}

func (s *Store) DecideDeviceAuth(ctx context.Context, userCode, userID string, approve bool, now time.Time) error {
	status := store.DeviceDenied
	if approve {
		status = store.DeviceApproved
	}
	tag, err := s.pool.Exec(ctx, `update device_auths set status = $3, user_id = $4
		where user_code = $1 and status = 'pending' and expires_at > $2`, userCode, now, string(status), userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) PollDeviceAuth(ctx context.Context, deviceCodeHash string, now time.Time, slowDown time.Duration, mint store.Token) (store.PollResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.PollResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	d, err := scanDevice(tx.QueryRow(ctx, `select `+deviceCols+` from device_auths where device_code_hash = $1 for update`, deviceCodeHash))
	if err != nil {
		return store.PollResult{}, err
	}
	res := store.PollResult{Interval: d.Interval}
	if !now.Before(d.ExpiresAt) || d.Status == store.DeviceConsumed {
		res.Status = store.PollExpired
		return res, nil
	}
	next := d.Status
	switch {
	case !d.LastPollAt.IsZero() && now.Sub(d.LastPollAt) < d.Interval:
		res.Status = store.PollSlowDown
		res.Interval += slowDown
	case d.Status == store.DevicePending:
		res.Status = store.PollPending
	case d.Status == store.DeviceDenied:
		res.Status, next = store.PollDenied, store.DeviceConsumed
	case d.Status == store.DeviceApproved:
		mint.UserID = d.UserID
		if mint.Name == "" {
			mint.Name = d.Client.Host
		}
		if err := createToken(ctx, tx, mint); err != nil {
			return store.PollResult{}, err
		}
		res.Status, res.UserID, next = store.PollApproved, d.UserID, store.DeviceConsumed
	default:
		res.Status = store.PollExpired
	}
	if _, err := tx.Exec(ctx, `update device_auths set status = $2, interval_ms = $3, last_poll_at = $4 where device_code_hash = $1`,
		deviceCodeHash, string(next), res.Interval.Milliseconds(), now); err != nil {
		return store.PollResult{}, err
	}
	return res, tx.Commit(ctx)
}

func (s *Store) CreateOrg(ctx context.Context, o store.Org, ownerID string, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `insert into orgs (id, name, created_at) values ($1, $2, $3)`, o.ID, o.Name, o.CreatedAt); err != nil {
		if isUniqueViolation(err) {
			return store.ErrConflict
		}
		return err
	}
	if _, err := tx.Exec(ctx, `insert into memberships (org_id, user_id, role, joined_at) values ($1, $2, 'owner', $3)`, o.ID, ownerID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanMemberships(rows pgx.Rows) ([]store.Membership, error) {
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.Membership, error) {
		var m store.Membership
		var role string
		err := r.Scan(&m.Org.ID, &m.Org.Name, &m.Org.CreatedAt, &role, &m.JoinedAt)
		m.Role = store.Role(role)
		return m, err
	})
}

func (s *Store) UserOrgs(ctx context.Context, userID string) ([]store.Membership, error) {
	rows, err := s.pool.Query(ctx, `select o.id, o.name, o.created_at, m.role, m.joined_at
		from memberships m join orgs o on o.id = m.org_id where m.user_id = $1 order by o.created_at, o.id`, userID)
	if err != nil {
		return nil, err
	}
	return scanMemberships(rows)
}

func (s *Store) Membership(ctx context.Context, orgID, userID string) (store.Membership, error) {
	rows, err := s.pool.Query(ctx, `select o.id, o.name, o.created_at, m.role, m.joined_at
		from memberships m join orgs o on o.id = m.org_id where m.org_id = $1 and m.user_id = $2`, orgID, userID)
	if err != nil {
		return store.Membership{}, err
	}
	ms, err := scanMemberships(rows)
	if err != nil {
		return store.Membership{}, err
	}
	if len(ms) == 0 {
		return store.Membership{}, store.ErrNotFound
	}
	return ms[0], nil
}

func (s *Store) OrgMembers(ctx context.Context, orgID string) ([]store.Member, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `select exists(select 1 from orgs where id = $1)`, orgID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, store.ErrNotFound
	}
	rows, err := s.pool.Query(ctx, `select `+userCols+`, m.role, m.joined_at
		from memberships m join users u on u.id = m.user_id where m.org_id = $1 order by m.joined_at, u.id`, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.Member, error) {
		var m store.Member
		var role string
		err := r.Scan(&m.User.ID, &m.User.Identity, &m.User.Login, &m.User.DisplayName, &m.User.CreatedAt, &role, &m.JoinedAt)
		m.Role = store.Role(role)
		return m, err
	})
}

func (s *Store) CreateInvite(ctx context.Context, inv store.Invite) error {
	_, err := s.pool.Exec(ctx, `insert into invites (token_hash, org_id, created_by, created_at, expires_at) values ($1, $2, $3, $4, $5)`,
		inv.Hash, inv.OrgID, inv.CreatedBy, inv.CreatedAt, inv.ExpiresAt)
	if isUniqueViolation(err) {
		return store.ErrConflict
	}
	return err
}

func (s *Store) AcceptInvite(ctx context.Context, hash, userID string, now time.Time) (store.Membership, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.Membership{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var orgID string
	err = tx.QueryRow(ctx, `select org_id from invites where token_hash = $1 and used_at is null and expires_at > $2 for update`, hash, now).Scan(&orgID)
	if err != nil {
		return store.Membership{}, notFound(err)
	}
	rows, err := tx.Query(ctx, `select o.id, o.name, o.created_at, m.role, m.joined_at
		from memberships m join orgs o on o.id = m.org_id where m.org_id = $1 and m.user_id = $2`, orgID, userID)
	if err != nil {
		return store.Membership{}, err
	}
	existing, err := scanMemberships(rows)
	if err != nil {
		return store.Membership{}, err
	}
	if len(existing) > 0 {
		return existing[0], nil // already a member: leave the invite unused
	}
	if _, err := tx.Exec(ctx, `update invites set used_at = $2, used_by = $3 where token_hash = $1`, hash, now, userID); err != nil {
		return store.Membership{}, err
	}
	if _, err := tx.Exec(ctx, `insert into memberships (org_id, user_id, role, joined_at) values ($1, $2, 'member', $3)`, orgID, userID, now); err != nil {
		return store.Membership{}, err
	}
	var m store.Membership
	if err := tx.QueryRow(ctx, `select id, name, created_at from orgs where id = $1`, orgID).Scan(&m.Org.ID, &m.Org.Name, &m.Org.CreatedAt); err != nil {
		return store.Membership{}, err
	}
	m.Role, m.JoinedAt = store.RoleMember, now
	return m, tx.Commit(ctx)
}

// notifyPayload is the NOTIFY payload: ids only, well under the 8000-byte limit.
type notifyPayload struct {
	Seq       int64     `json:"seq"`
	UserID    string    `json:"user_id"`
	Type      string    `json:"type"`
	EntityID  string    `json:"id"`
	Developer string    `json:"developer"`
	At        time.Time `json:"at"`
}

func (s *Store) Ingest(ctx context.Context, in store.IngestInput) (store.IngestResult, error) {
	var res store.IngestResult
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	b := &pgx.Batch{}
	for _, r := range in.Records {
		b.Queue(`insert into entities as e (user_id, kind, key, rev, data, updated_at) values ($1, $2, $3, $4, $5, $6)
			on conflict (user_id, kind, key) do update set rev = excluded.rev, data = excluded.data, updated_at = excluded.updated_at
			where e.rev <> excluded.rev
			returning 1`, in.UserID, string(r.Kind), r.Key, r.Rev, []byte(r.Data), in.Now)
	}
	br := tx.SendBatch(ctx, b)
	var accepted []store.Record
	for _, r := range in.Records {
		var one int
		err := br.QueryRow().Scan(&one)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			res.Unchanged++
		case err != nil:
			_ = br.Close()
			return store.IngestResult{}, err
		default:
			res.Accepted++
			accepted = append(accepted, r)
		}
	}
	if err := br.Close(); err != nil {
		return store.IngestResult{}, err
	}

	for _, e := range store.EventsFor(accepted, in) {
		if err := tx.QueryRow(ctx, `insert into events (user_id, type, entity_id, developer, created_at) values ($1, $2, $3, $4, $5) returning seq`,
			e.UserID, e.Type, e.EntityID, e.Developer, e.At).Scan(&e.Seq); err != nil {
			return store.IngestResult{}, err
		}
		payload, err := json.Marshal(notifyPayload{Seq: e.Seq, UserID: e.UserID, Type: e.Type, EntityID: e.EntityID, Developer: e.Developer, At: e.At})
		if err != nil {
			return store.IngestResult{}, err
		}
		// Delivered to listeners only when the transaction commits.
		if _, err := tx.Exec(ctx, `select pg_notify($1, $2)`, Channel, string(payload)); err != nil {
			return store.IngestResult{}, err
		}
		res.Events = append(res.Events, e)
	}
	return res, tx.Commit(ctx)
}

func (s *Store) Records(ctx context.Context, userIDs []string) ([]store.Record, error) {
	rows, err := s.pool.Query(ctx, `select user_id, kind, key, rev, data, updated_at from entities
		where user_id = any($1) order by user_id, kind, key`, userIDs)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.Record, error) {
		var rec store.Record
		var kind string
		var data []byte
		err := r.Scan(&rec.UserID, &kind, &rec.Key, &rec.Rev, &data, &rec.UpdatedAt)
		rec.Kind, rec.Data = store.Kind(kind), data
		return rec, err
	})
}

func (s *Store) EventsAfter(ctx context.Context, userIDs []string, after int64, limit int) ([]store.Event, error) {
	q := `select seq, user_id, type, entity_id, developer, created_at from events where seq > $1 and ($2::text[] is null or user_id = any($2)) order by seq limit $3`
	rows, err := s.pool.Query(ctx, q, after, userIDs, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.Event, error) {
		var e store.Event
		err := r.Scan(&e.Seq, &e.UserID, &e.Type, &e.EntityID, &e.Developer, &e.At)
		return e, err
	})
}

func (s *Store) SetReviewDecision(ctx context.Context, d store.ReviewDecision) error {
	var note *string
	if d.Note != "" {
		note = &d.Note
	}
	_, err := s.pool.Exec(ctx, `insert into review_decisions as r (user_id, resource_id, state, note, decided_at)
		values ($1, $2, $3, $4, $5)
		on conflict (user_id, resource_id) do update
		set state = excluded.state, note = excluded.note, decided_at = excluded.decided_at
		where r.decided_at <= excluded.decided_at`,
		d.UserID, d.ResourceID, d.State, note, d.DecidedAt)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23503" { // foreign key: unknown user
		return store.ErrNotFound
	}
	return err
}

func (s *Store) ReviewDecisions(ctx context.Context, userIDs []string) ([]store.ReviewDecision, error) {
	rows, err := s.pool.Query(ctx, `select user_id, resource_id, state, coalesce(note, ''), decided_at from review_decisions
		where user_id = any($1) order by user_id, resource_id`, userIDs)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.ReviewDecision, error) {
		var d store.ReviewDecision
		err := r.Scan(&d.UserID, &d.ResourceID, &d.State, &d.Note, &d.DecidedAt)
		return d, err
	})
	if out == nil && err == nil {
		out = []store.ReviewDecision{}
	}
	return out, err
}

func (s *Store) AddProviderRequest(ctx context.Context, req store.ProviderRequest) error {
	_, err := s.pool.Exec(ctx, `insert into provider_requests (provider, email, user_id, created_at) values ($1, $2, $3, $4)`,
		req.Provider, nullable(req.Email), nullable(req.UserID), req.At)
	return err
}

// nullable stores "" as NULL.
func nullable(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// MaxSeq returns the newest event sequence number.
func (s *Store) MaxSeq(ctx context.Context) (int64, error) {
	var seq int64
	err := s.pool.QueryRow(ctx, `select coalesce(max(seq), 0) from events`).Scan(&seq)
	return seq, err
}
