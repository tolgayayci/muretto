package pgstore

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrationLock is the pg_advisory_xact_lock key, so two server instances
// starting at once never apply migrations concurrently ("muretto" in ASCII).
const migrationLock int64 = 0x6d757265_74746f

type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, n := range names {
		base := strings.TrimPrefix(n, "migrations/")
		num, _, ok := strings.Cut(base, "_")
		if !ok {
			return nil, fmt.Errorf("migration %s: name must be NNNN_title.sql", base)
		}
		v, err := strconv.Atoi(num)
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", base, err)
		}
		b, err := migrationFiles.ReadFile(n)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: v, name: base, sql: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	for i := 1; i < len(out); i++ {
		if out[i].version == out[i-1].version {
			return nil, fmt.Errorf("duplicate migration version %d", out[i].version)
		}
	}
	return out, nil
}

// Migrate applies pending migrations in one transaction under an advisory lock.
func Migrate(ctx context.Context, pool *pgxpool.Pool) (applied []string, err error) {
	ms, err := loadMigrations()
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, migrationLock); err != nil {
		return nil, fmt.Errorf("advisory lock: %w", err)
	}
	if _, err := tx.Exec(ctx, `create table if not exists schema_migrations (
		version int primary key, name text not null, applied_at timestamptz not null default now())`); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `select version from schema_migrations`)
	if err != nil {
		return nil, err
	}
	done, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return nil, err
	}
	have := map[int]bool{}
	for _, v := range done {
		have[v] = true
	}
	for _, m := range ms {
		if have[m.version] {
			continue
		}
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			return nil, fmt.Errorf("migration %s: %w", m.name, err)
		}
		if _, err := tx.Exec(ctx, `insert into schema_migrations (version, name) values ($1, $2)`, m.version, m.name); err != nil {
			return nil, err
		}
		applied = append(applied, m.name)
	}
	return applied, tx.Commit(ctx)
}
