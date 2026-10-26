package broker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// SupabaseRLSCatalogQuery is the only SQL Muretto ever sends, and only when the
// user opts in (docs/research/supabase.md Q3). It is one constant SELECT on
// pg_catalog: no parameters, no user input, no ';', no SET, and no functions
// other than has_*_privilege. It runs through
// POST /v1/projects/{ref}/database/query/read-only, as supabase_read_only_user.
const SupabaseRLSCatalogQuery = `select n.nspname as schema,
       c.relname as name,
       c.relrowsecurity as rls_enabled,
       c.relforcerowsecurity as rls_forced,
       pg_catalog.has_schema_privilege('anon', n.oid, 'USAGE') as anon_schema_usage,
       pg_catalog.has_table_privilege('anon', c.oid, 'SELECT') as anon_select,
       pg_catalog.has_table_privilege('authenticated', c.oid, 'SELECT') as authenticated_select
from pg_catalog.pg_class c
join pg_catalog.pg_namespace n on n.oid = c.relnamespace
where c.relkind in ('r', 'p')
  and n.nspname not in ('auth','cron','extensions','graphql','graphql_public','information_schema','net','pgbouncer','pg_catalog','pgmq','pgsodium','pgsodium_masks','realtime','storage','supabase_functions','supabase_migrations','vault','pgtle','pgroonga','repack','tiger','topology','_timescaledb_cache','_timescaledb_catalog','_timescaledb_config','_timescaledb_internal')
  and n.nspname not like 'pg\_%'
order by 1, 2`

// pinnedBodies are the only POST bodies the broker will ever send, by name.
// Adapter YAML may refer to a name; it can never add a body or a hash.
var pinnedBodies = map[string][]byte{
	"supabase_rls_catalog_v1": mustJSON(map[string]string{"query": SupabaseRLSCatalogQuery}),
}

// pinnedByHash maps a body's hex sha256 to its name.
var pinnedByHash = func() map[string]string {
	m := make(map[string]string, len(pinnedBodies))
	for name, body := range pinnedBodies {
		sum := sha256.Sum256(body)
		m[hex.EncodeToString(sum[:])] = name
	}
	return m
}()

// PinnedBody returns a pinned POST body and its hex sha256 by name.
func PinnedBody(name string) (body []byte, sha string, ok bool) {
	b, ok := pinnedBodies[name]
	if !ok {
		return nil, "", false
	}
	sum := sha256.Sum256(b)
	return append([]byte(nil), b...), hex.EncodeToString(sum[:]), true
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// deniedPaths are refused even if an allowlist rule matches them: they return
// secrets, source code or decrypted values, or they write. "*" matches one
// segment; a trailing "**" matches the rest of the path (including nothing).
var deniedPaths = []struct{ host, pattern string }{
	// Supabase (docs/research/supabase.md "Never call" and Recommendations).
	{"api.supabase.com", "/v1/branches/**"},                    // returns db_pass and jwt_secret
	{"api.supabase.com", "/v1/projects/*/functions/*/body"},    // function source code
	{"api.supabase.com", "/v1/projects/*/config/auth/**"},      // smtp_pass, provider secrets
	{"api.supabase.com", "/v1/projects/*/secrets/**"},          // secret values
	{"api.supabase.com", "/v1/projects/*/api-keys/**"},         // API keys
	{"api.supabase.com", "/v2/projects/*/config/**"},           // effective auth config
	{"api.supabase.com", "/v1/projects/*/database/query"},      // write-scoped SQL endpoint
	{"api.supabase.com", "/v1/projects/*/database/migrations"}, // POST applies migrations
	// Vercel (docs/research/vercel.md §3.4).
	{"api.vercel.com", "/*/projects/*/env/*"}, // one env var: decrypted value
	{"api.vercel.com", "/v1/env/**"},          // shared env vars carry values
}

func denied(host string, segs []string) string {
	for _, d := range deniedPaths {
		if d.host != host {
			continue
		}
		if globMatch(strings.Split(strings.TrimPrefix(d.pattern, "/"), "/"), segs) {
			return d.pattern
		}
	}
	return ""
}

func globMatch(pat, segs []string) bool {
	for i, p := range pat {
		if p == "**" {
			return true
		}
		if i >= len(segs) {
			return false
		}
		if p != "*" && p != segs[i] {
			return false
		}
	}
	return len(pat) == len(segs)
}
