// Package store is Muretto's local SQLite store (~/.muretto/muretto.db):
// sessions, actions with identifiers, resources with history, checks,
// findings, costs, review decisions, sync revs and scan timestamps.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tolgayayci/muretto/internal/contract"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registered as "sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store wraps the database.
type Store struct {
	db *sql.DB
}

// Open opens (and creates or migrates) the store at path with WAL enabled.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- Muretto's own db path
	if err != nil {
		return nil, err
	}
	f.Close()
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	if version > len(names) {
		return fmt.Errorf("database schema version %d is newer than this muretto (%d)", version, len(names))
	}
	for i := version; i < len(names); i++ {
		body, err := migrations.ReadFile(names[i])
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", names[i], err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func enc(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// ---- sessions and actions ----

// Action is an agent action plus the redacted raw material adapter signatures
// match against, kept so actions can be re-extracted when adapters change.
type Action struct {
	Action    contract.AgentAction
	Input     any
	Output    string
	Extracted bool
}

// SaveBatch stores sessions (merged with what is stored) and actions,
// deduplicated on id with the same precedence as observe.Merge: a hook record
// beats a transcript copy, then the earlier action time, then the session
// that started first. A remaining tie between two sessions keeps the lower
// session id and marks the action owner-ambiguous, as do the ids passed in
// ambiguous. It is idempotent: re-ingesting a batch changes nothing, so a
// crash between commit and observe.Ack is safe.
func (s *Store) SaveBatch(ctx context.Context, sessions []contract.Session, actions []Action, ambiguous ...string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, sess := range sessions {
		var body string
		err := tx.QueryRowContext(ctx, "SELECT body FROM sessions WHERE id = ?", sess.ID).Scan(&body)
		if err == nil {
			var old contract.Session
			if err := json.Unmarshal([]byte(body), &old); err == nil {
				sess = MergeSession(old, sess)
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		b, err := enc(sess)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(id, started_at, developer_id, body) VALUES(?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET started_at=excluded.started_at, developer_id=excluded.developer_id, body=excluded.body`,
			sess.ID, ts(sess.StartedAt), sess.DeveloperID, b); err != nil {
			return err
		}
	}
	started := func(id string) (time.Time, bool) {
		var v string
		if err := tx.QueryRowContext(ctx, "SELECT started_at FROM sessions WHERE id = ?", id).Scan(&v); err != nil {
			return time.Time{}, false
		}
		t, err := time.Parse(time.RFC3339Nano, v)
		return t, err == nil
	}
	for _, a := range actions {
		if a.Action.Identifiers == nil {
			a.Action.Identifiers = []contract.Identifier{}
		}
		var oldSession, oldSource, oldAt string
		err := tx.QueryRowContext(ctx, "SELECT session_id, source, occurred_at FROM actions WHERE id = ?", a.Action.ID).Scan(&oldSession, &oldSource, &oldAt)
		if err == nil {
			replace := false
			if oldSession != a.Action.SessionID {
				var tie bool
				replace, tie = beats(a.Action, oldSession, oldSource, oldAt, started)
				if tie {
					ambiguous = append(ambiguous, a.Action.ID)
				}
			} else {
				replace = oldSource != string(contract.AgentActionSourceHook) || a.Action.Source == contract.AgentActionSourceHook
			}
			if !replace {
				continue
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		body, err := enc(a.Action)
		if err != nil {
			return err
		}
		in, err := enc(a.Input)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO actions(id, session_id, occurred_at, source, body, raw_input, raw_output, extracted)
			VALUES(?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET session_id=excluded.session_id, occurred_at=excluded.occurred_at, source=excluded.source,
			body=excluded.body, raw_input=excluded.raw_input, raw_output=excluded.raw_output, extracted=excluded.extracted`,
			a.Action.ID, a.Action.SessionID, ts(a.Action.OccurredAt), string(a.Action.Source), body, in, a.Output, boolInt(a.Extracted)); err != nil {
			return err
		}
	}
	for _, id := range ambiguous {
		if _, err := tx.ExecContext(ctx, "UPDATE actions SET owner_ambiguous = 1 WHERE id = ?", id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// beats reports whether action a, held by another session, should replace
// the stored copy, and whether the two copies are indistinguishable.
func beats(a contract.AgentAction, oldSession, oldSource, oldAt string, started func(string) (time.Time, bool)) (replace, tie bool) {
	hook := func(src string) int {
		if src == string(contract.AgentActionSourceHook) {
			return 0
		}
		return 1
	}
	if hn, ho := hook(string(a.Source)), hook(oldSource); hn != ho {
		return hn < ho, false
	}
	if ot, err := time.Parse(time.RFC3339Nano, oldAt); err == nil && !a.OccurredAt.Equal(ot) {
		return a.OccurredAt.Before(ot), false
	}
	ns, nok := started(a.SessionID)
	olds, ook := started(oldSession)
	switch {
	case nok && !ook:
		return true, false
	case !nok && ook:
		return false, false
	case nok && ook && !ns.Equal(olds):
		return ns.Before(olds), false
	}
	return a.SessionID < oldSession, true
}

// AmbiguousActions returns the ids of actions whose owning session cannot be
// told apart from another session's copy.
func (s *Store) AmbiguousActions(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM actions WHERE owner_ambiguous = 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

var titleRank = map[contract.SessionTitleSource]int{
	contract.SessionTitleSourceCustomTitle: 3,
	contract.SessionTitleSourceFirstPrompt: 2,
	contract.SessionTitleSourceAiTitle:     1,
	contract.SessionTitleSourceNone:        0,
}

// MergeSession combines two observations of the same session.
func MergeSession(old, nw contract.Session) contract.Session {
	out := old
	if !nw.StartedAt.IsZero() && (out.StartedAt.IsZero() || nw.StartedAt.Before(out.StartedAt)) {
		out.StartedAt = nw.StartedAt
	}
	later := func(a, b *time.Time) *time.Time {
		if a == nil {
			return b
		}
		if b != nil && b.After(*a) {
			return b
		}
		return a
	}
	out.EndedAt = later(out.EndedAt, nw.EndedAt)
	out.LastActivityAt = later(out.LastActivityAt, nw.LastActivityAt)
	if nw.EndReason != nil {
		out.EndReason = nw.EndReason
	}
	if out.AgentVersion == nil {
		out.AgentVersion = nw.AgentVersion
	}
	if out.GitBranch == nil {
		out.GitBranch = nw.GitBranch
	}
	if out.Model == nil {
		out.Model = nw.Model
	}
	if out.Repo == nil {
		out.Repo = nw.Repo
	}
	if out.DeveloperID == "" {
		out.DeveloperID = nw.DeveloperID
	}
	if nw.TaskTitle != nil && titleRank[nw.TitleSource] > titleRank[out.TitleSource] {
		out.TaskTitle, out.TitleSource = nw.TaskTitle, nw.TitleSource
	}
	seen := map[contract.SessionSourcesElem]bool{}
	var src []contract.SessionSourcesElem
	for _, x := range append(append([]contract.SessionSourcesElem{}, old.Sources...), nw.Sources...) {
		if !seen[x] {
			seen[x] = true
			src = append(src, x)
		}
	}
	sort.Slice(src, func(i, j int) bool { return src[i] < src[j] })
	out.Sources = src
	return out
}

// Sessions returns every stored session, newest first.
func (s *Store) Sessions(ctx context.Context) ([]contract.Session, error) {
	return queryJSON[contract.Session](ctx, s.db, "SELECT body FROM sessions ORDER BY started_at DESC, id")
}

// Actions returns every stored action, oldest first.
func (s *Store) Actions(ctx context.Context) ([]contract.AgentAction, error) {
	return queryJSON[contract.AgentAction](ctx, s.db, "SELECT body FROM actions ORDER BY occurred_at, id")
}

// RawActions returns stored actions with their raw material. With pending
// set, only actions adapter signatures have not run on yet.
func (s *Store) RawActions(ctx context.Context, pending bool) ([]Action, error) {
	q := "SELECT body, COALESCE(raw_input,'null'), COALESCE(raw_output,''), extracted FROM actions"
	if pending {
		q += " WHERE extracted = 0"
	}
	rows, err := s.db.QueryContext(ctx, q+" ORDER BY occurred_at, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Action
	for rows.Next() {
		var body, in, outText string
		var ex int
		if err := rows.Scan(&body, &in, &outText, &ex); err != nil {
			return nil, err
		}
		var a Action
		if err := json.Unmarshal([]byte(body), &a.Action); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(in), &a.Input)
		a.Output, a.Extracted = outText, ex == 1
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetIdentifiers replaces the identifiers of stored actions and marks them extracted.
func (s *Store) SetIdentifiers(ctx context.Context, ids map[string][]contract.Identifier) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, idents := range ids {
		var body string
		if err := tx.QueryRowContext(ctx, "SELECT body FROM actions WHERE id = ?", id).Scan(&body); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return err
		}
		var a contract.AgentAction
		if err := json.Unmarshal([]byte(body), &a); err != nil {
			return err
		}
		if idents == nil {
			idents = []contract.Identifier{}
		}
		a.Identifiers = idents
		for _, x := range idents {
			if a.ProviderGuess == nil {
				p := x.Provider
				a.ProviderGuess = &p
			}
		}
		b, err := enc(a)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE actions SET body = ?, extracted = 1 WHERE id = ?", b, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---- scans ----

// Scan is one successful read-only scan of one account (adapters.ScanResult).
type Scan struct {
	Resources []contract.Resource
	Findings  []contract.ExposureFinding
	Costs     []contract.CostRecord
	Checks    []contract.ProviderCheck
}

// ParentProviderID returns the provider id part of a resource's parent id.
func ParentProviderID(r contract.Resource) string {
	if r.ParentID == nil {
		return ""
	}
	_, pid, _ := strings.Cut(*r.ParentID, ":")
	return pid
}

// CheckCovers reports whether check c (same provider and account) lists
// resources of r's kind in r's scope.
func CheckCovers(c contract.ProviderCheck, kind, scope, accountID string) bool {
	if c.Kind != kind {
		return false
	}
	if c.Scope == nil || *c.Scope == "" {
		return true
	}
	return *c.Scope == scope || *c.Scope == accountID
}

func scopeOf(resourceID string, byID map[string]contract.Resource) (kind, scope string) {
	kind, pid, _ := strings.Cut(resourceID, ":")
	if r, ok := byID[resourceID]; ok {
		return kind, ParentProviderID(r)
	}
	if i := strings.Index(pid, "/"); i > 0 {
		return kind, pid[:i]
	}
	return kind, ""
}

// ApplyScan records a scan of provider/accountID at now. Resources missing
// from a successful list check are marked gone (history is kept); resources
// that come back keep gone_at so attribution can tell they were recreated.
func (s *Store) ApplyScan(ctx context.Context, provider, accountID string, sc Scan, now time.Time) error {
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	existing := map[string]contract.Resource{}
	rows, err := tx.QueryContext(ctx, "SELECT body FROM resources WHERE provider = ? AND account_id = ?", provider, accountID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			rows.Close()
			return err
		}
		var r contract.Resource
		if err := json.Unmarshal([]byte(body), &r); err == nil {
			existing[r.ID] = r
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	event := func(id, ev string) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO resource_events(resource_id, at, event) VALUES(?,?,?)", id, ts(now), ev)
		return err
	}
	put := func(r contract.Resource) error {
		if r.Attributes == nil {
			r.Attributes = contract.ResourceAttributes{}
		}
		b, err := enc(r)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO resources(id, provider, account_id, kind, scope, existence, body) VALUES(?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET provider=excluded.provider, account_id=excluded.account_id, kind=excluded.kind,
			scope=excluded.scope, existence=excluded.existence, body=excluded.body`,
			r.ID, r.Provider, r.AccountID, r.Kind, ParentProviderID(r), string(r.Existence), b)
		return err
	}

	seen := map[string]contract.Resource{}
	for _, r := range sc.Resources {
		r.Provider, r.AccountID = provider, accountID
		r.LastSeenAt = now
		r.Existence = contract.ResourceExistenceConfirmed
		if r.SeenVia == "" {
			r.SeenVia = contract.ResourceSeenViaList
		}
		old, had := existing[r.ID]
		switch {
		case !had:
			r.FirstSeenAt = now
			r.GoneAt = nil
			if err := event(r.ID, "seen"); err != nil {
				return err
			}
		default:
			r.FirstSeenAt = old.FirstSeenAt
			r.GoneAt = old.GoneAt
			if old.Existence == contract.ResourceExistenceGone {
				if err := event(r.ID, "returned"); err != nil {
					return err
				}
			}
		}
		seen[r.ID] = r
		if err := put(r); err != nil {
			return err
		}
	}

	for id, r := range existing {
		if _, ok := seen[id]; ok || r.Existence == contract.ResourceExistenceGone {
			continue
		}
		listed := false
		for _, c := range sc.Checks {
			if c.Status == contract.ProviderCheckStatusOk && c.RuleID == nil && CheckCovers(c, r.Kind, ParentProviderID(r), accountID) {
				listed = true
			}
		}
		switch {
		case listed:
			g := now
			r.Existence, r.GoneAt = contract.ResourceExistenceGone, &g
			if err := event(id, "gone"); err != nil {
				return err
			}
		case r.SeenVia == contract.ResourceSeenViaAdvisor && r.Existence == contract.ResourceExistenceConfirmed && advisorRan(sc.Checks, r, accountID):
			// The advisor only lists flagged resources; absence is not deletion.
			r.Existence = contract.ResourceExistenceUnconfirmed
			if err := event(id, "unconfirmed"); err != nil {
				return err
			}
		default:
			continue
		}
		if err := put(r); err != nil {
			return err
		}
	}

	allByID := map[string]contract.Resource{}
	for id, r := range existing {
		allByID[id] = r
	}
	for id, r := range seen {
		allByID[id] = r
	}

	for _, c := range sc.Checks {
		c.Provider, c.AccountID = provider, accountID
		scope, rule := "", ""
		if c.Scope != nil {
			scope = *c.Scope
		}
		if c.RuleID != nil {
			rule = *c.RuleID
		}
		if c.Status == contract.ProviderCheckStatusOk {
			if c.LastSuccessAt == nil {
				t := c.AttemptedAt
				c.LastSuccessAt = &t
			}
			if rule != "" {
				q := "DELETE FROM findings WHERE provider = ? AND account_id = ? AND kind = ? AND rule_id = ?"
				args := []any{provider, accountID, c.Kind, rule}
				if scope != "" {
					q += " AND scope = ?"
					args = append(args, scope)
				}
				if _, err := tx.ExecContext(ctx, q, args...); err != nil {
					return err
				}
			}
		} else if c.LastSuccessAt == nil {
			var body string
			err := tx.QueryRowContext(ctx, "SELECT body FROM checks WHERE provider=? AND account_id=? AND kind=? AND scope=? AND rule_id=?",
				provider, accountID, c.Kind, scope, rule).Scan(&body)
			if err == nil {
				var old contract.ProviderCheck
				if json.Unmarshal([]byte(body), &old) == nil {
					c.LastSuccessAt = old.LastSuccessAt
				}
			}
		}
		b, err := enc(c)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO checks(provider, account_id, kind, scope, rule_id, body) VALUES(?,?,?,?,?,?)
			ON CONFLICT(provider, account_id, kind, scope, rule_id) DO UPDATE SET body=excluded.body`,
			provider, accountID, c.Kind, scope, rule, b); err != nil {
			return err
		}
	}

	for _, f := range sc.Findings {
		kind, scope := scopeOf(f.ResourceID, allByID)
		b, err := enc(f)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO findings(id, provider, account_id, kind, scope, rule_id, body) VALUES(?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET provider=excluded.provider, account_id=excluded.account_id, kind=excluded.kind,
			scope=excluded.scope, rule_id=excluded.rule_id, body=excluded.body`,
			f.ID, provider, accountID, kind, scope, f.RuleID, b); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM costs WHERE provider = ? AND account_id = ?", provider, accountID); err != nil {
		return err
	}
	for _, c := range sc.Costs {
		if c.Source == contract.CostRecordSourceUnknown {
			c.AmountMicros = nil
		}
		interval := ""
		if c.Interval != nil {
			interval = string(*c.Interval)
		}
		b, err := enc(c)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO costs(resource_id, source, interval, provider, account_id, body) VALUES(?,?,?,?,?,?)
			ON CONFLICT(resource_id, source, interval) DO UPDATE SET body=excluded.body`,
			c.ResourceID, string(c.Source), interval, provider, accountID, b); err != nil {
			return err
		}
	}
	if err := setMeta(ctx, tx, ScanKey(provider, accountID), ts(now)); err != nil {
		return err
	}
	return tx.Commit()
}

func advisorRan(checks []contract.ProviderCheck, r contract.Resource, accountID string) bool {
	for _, c := range checks {
		if c.Status == contract.ProviderCheckStatusOk && c.RuleID != nil && CheckCovers(c, r.Kind, ParentProviderID(r), accountID) {
			return true
		}
	}
	return false
}

// RecordCheckFailure stores failed checks for an account whose scan failed
// as a whole, keeping last_success_at and all previous data.
func (s *Store) RecordCheckFailure(ctx context.Context, provider, accountID string, now time.Time, code contract.ProviderCheckErrorCode, msg string) error {
	checks, err := s.Checks(ctx)
	if err != nil {
		return err
	}
	var failed []contract.ProviderCheck
	for _, c := range checks {
		if c.Provider == provider && c.AccountID == accountID {
			c.Status = contract.ProviderCheckStatusError
			c.AttemptedAt = now.UTC()
			cd, m := code, msg
			c.ErrorCode, c.Error = &cd, &m
			failed = append(failed, c)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range failed {
		scope, rule := "", ""
		if c.Scope != nil {
			scope = *c.Scope
		}
		if c.RuleID != nil {
			rule = *c.RuleID
		}
		b, err := enc(c)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE checks SET body = ? WHERE provider=? AND account_id=? AND kind=? AND scope=? AND rule_id=?",
			b, provider, accountID, c.Kind, scope, rule); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Resources returns every stored resource, including gone ones.
func (s *Store) Resources(ctx context.Context) ([]contract.Resource, error) {
	return queryJSON[contract.Resource](ctx, s.db, "SELECT body FROM resources ORDER BY id")
}

// Findings returns the latest findings.
func (s *Store) Findings(ctx context.Context) ([]contract.ExposureFinding, error) {
	return queryJSON[contract.ExposureFinding](ctx, s.db, "SELECT body FROM findings ORDER BY id")
}

// Costs returns the latest cost records.
func (s *Store) Costs(ctx context.Context) ([]contract.CostRecord, error) {
	return queryJSON[contract.CostRecord](ctx, s.db, "SELECT body FROM costs ORDER BY resource_id, source, interval")
}

// Checks returns the latest check per (provider, account, kind, scope, rule).
func (s *Store) Checks(ctx context.Context) ([]contract.ProviderCheck, error) {
	return queryJSON[contract.ProviderCheck](ctx, s.db, "SELECT body FROM checks ORDER BY provider, account_id, kind, scope, rule_id")
}

// Event is one entry in a resource's history.
type Event struct {
	At    time.Time
	Event string
}

// ResourceEvents returns a resource's history, oldest first.
func (s *Store) ResourceEvents(ctx context.Context, id string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT at, event FROM resource_events WHERE resource_id = ? ORDER BY at, rowid", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var at, ev string
		if err := rows.Scan(&at, &ev); err != nil {
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339Nano, at)
		out = append(out, Event{At: t, Event: ev})
	}
	return out, rows.Err()
}

// ---- reviews ----

// Reviews returns recorded review decisions.
func (s *Store) Reviews(ctx context.Context) ([]contract.ReviewItem, error) {
	return queryJSON[contract.ReviewItem](ctx, s.db, "SELECT body FROM reviews ORDER BY resource_id")
}

// SetReview records a human decision. Muretto never acts on it.
func (s *Store) SetReview(ctx context.Context, r contract.ReviewItem) error {
	b, err := enc(r)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO reviews(resource_id, body) VALUES(?,?)
		ON CONFLICT(resource_id) DO UPDATE SET body=excluded.body`, r.ResourceID, b)
	return err
}

// ---- meta and sync ----

// ScanKey is the meta key holding the time of the last successful scan.
func ScanKey(provider, accountID string) string { return "scan:" + provider + ":" + accountID }

// Meta keys used by the CLI.
const (
	MetaTranscriptsScannedAt = "transcripts_scanned_at"
	MetaExtractVersion       = "extract_version"
)

// Meta returns a meta value.
func (s *Store) Meta(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

// SetMeta stores a meta value.
func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	return setMeta(ctx, s.db, key, value)
}

// MetaTime reads a timestamp meta value; the zero time when unset.
func (s *Store) MetaTime(ctx context.Context, key string) (time.Time, error) {
	v, ok, err := s.Meta(ctx, key)
	if err != nil || !ok {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, v)
}

type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

func setMeta(ctx context.Context, db execer, key, value string) error {
	_, err := db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// SyncRevs returns the revs last accepted by the sync server.
func (s *Store) SyncRevs(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key, rev FROM sync_revs")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetSyncRevs replaces the stored revs.
func (s *Store) SetSyncRevs(ctx context.Context, revs map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM sync_revs"); err != nil {
		return err
	}
	for k, v := range revs {
		if _, err := tx.ExecContext(ctx, "INSERT INTO sync_revs(key, rev) VALUES(?,?)", k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func queryJSON[T any](ctx context.Context, db *sql.DB, q string, args ...any) ([]T, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var v T
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
