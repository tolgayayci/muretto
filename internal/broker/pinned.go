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
	// GitHub (docs/research/github.md "Never call").
	{"api.github.com", "/repos/*/*/actions/variables/*"},        // one variable: plaintext value
	{"api.github.com", "/orgs/*/actions/variables/*"},           // one org variable: plaintext value
	{"api.github.com", "/repos/*/*/environments/*/variables/*"}, // one environment variable
	{"api.github.com", "/repos/*/*/contents/**"},                // source code
	{"api.github.com", "/repos/*/*/git/blobs/**"},               // source code
	{"api.github.com", "/repos/*/*/tarball/**"},                 // source archive
	{"api.github.com", "/repos/*/*/zipball/**"},                 // source archive
	{"api.github.com", "/repos/*/*/readme/**"},                  // file contents
	{"api.github.com", "/repos/*/*/actions/runs/*/logs"},        // logs may hold secrets
	{"api.github.com", "/repos/*/*/actions/jobs/*/logs"},        // logs may hold secrets
	{"api.github.com", "/repos/*/*/actions/artifacts/**"},       // build artifacts
	// Cloudflare (docs/research/cloudflare.md "Never call"); a Read token
	// can read these.
	{"api.cloudflare.com", "/client/v4/accounts/*/r2/buckets/*/objects/**"},                         // object contents
	{"api.cloudflare.com", "/client/v4/accounts/*/storage/kv/namespaces/*/values/**"},               // KV values
	{"api.cloudflare.com", "/client/v4/accounts/*/storage/kv/namespaces/*/bulk/**"},                 // KV values
	{"api.cloudflare.com", "/client/v4/accounts/*/workers/scripts/*"},                               // script source
	{"api.cloudflare.com", "/client/v4/accounts/*/workers/scripts/*/content/**"},                    // script source
	{"api.cloudflare.com", "/client/v4/accounts/*/workers/scripts/*/versions/*"},                    // version modules
	{"api.cloudflare.com", "/client/v4/accounts/*/workers/workers/*/versions/**"},                   // version modules
	{"api.cloudflare.com", "/client/v4/accounts/*/workers/dispatch/namespaces/*/scripts/*/content"}, // script source
	{"api.cloudflare.com", "/client/v4/accounts/*/cfd_tunnel/*/token"},                              // tunnel run token
	{"api.cloudflare.com", "/client/v4/accounts/*/cfd_tunnel/*/management"},                         // management token
	{"api.cloudflare.com", "/client/v4/accounts/*/pages/projects/*/upload-token"},                   // upload JWT
	{"api.cloudflare.com", "/client/v4/accounts/*/d1/database/*/query"},                             // data
	{"api.cloudflare.com", "/client/v4/accounts/*/d1/database/*/raw"},                               // data
	{"api.cloudflare.com", "/client/v4/accounts/*/d1/database/*/export"},                            // data
	{"api.cloudflare.com", "/client/v4/accounts/*/hyperdrive/configs/**"},                           // origin passwords
	// Neon (docs/research/neon.md "Never call").
	{"console.neon.tech", "/api/v2/projects/*/connection_uri"},                     // connection string with password
	{"console.neon.tech", "/api/v2/projects/*/branches/*/roles/*/reveal_password"}, // role password
	{"console.neon.tech", "/api/v2/projects/*/branches/*/roles/*/reset_password"},  // writes, returns a password
	{"console.neon.tech", "/api/v2/projects/*/branches/*/credentials/**"},          // credentials
	{"console.neon.tech", "/api/v2/projects/*/branches/*/realtime/**"},             // realtime secrets
	{"console.neon.tech", "/api/v2/projects/*/branches/*/auth/**"},                 // auth secrets
	{"console.neon.tech", "/api/v2/api_keys/**"},                                   // API keys
	{"console.neon.tech", "/api/v2/organizations/*/api_keys/**"},                   // org API keys
	// Stripe (docs/research/stripe.md "Never call").
	{"api.stripe.com", "/v1/apps/secrets/**"},   // plaintext app secrets
	{"api.stripe.com", "/v1/ephemeral_keys/**"}, // ephemeral keys
	{"api.stripe.com", "/v1/accounts/**"},       // Connect accounts: PII, not used
	{"api.stripe.com", "/v1/issuing/cards/**"},  // card details
	{"api.stripe.com", "/v2/core/vault/**"},     // vaulted data
	// Render (docs/research/render.md "Never call").
	{"api.render.com", "/v1/postgres/*/connection-info"},
	{"api.render.com", "/v1/postgres/*/credentials/**"},
	{"api.render.com", "/v1/postgres/*/export/**"},
	{"api.render.com", "/v1/key-value/*/connection-info"},
	{"api.render.com", "/v1/redis/*/connection-info"},
	{"api.render.com", "/v1/services/*/env-vars/**"},
	{"api.render.com", "/v1/services/*/secret-files/**"},
	{"api.render.com", "/v1/env-groups/*"}, // one group, with values
	{"api.render.com", "/v1/env-groups/*/env-vars/**"},
	{"api.render.com", "/v1/env-groups/*/secret-files/**"},
	{"api.render.com", "/v1/build-sources/*/env-vars/**"},
	{"api.render.com", "/v1/build-sources/*/secret-files/**"},
	// PlanetScale (docs/research/planetscale.md "Never call").
	{"api.planetscale.com", "/v1/organizations/*/databases/*/webhooks/**"},        // webhook secrets
	{"api.planetscale.com", "/v1/organizations/*/oauth-applications/*/tokens/**"}, // OAuth tokens
	{"api.planetscale.com", "/v1/organizations/*/databases/*/branches/*/roles/*"}, // one role: connection details
}

// DeniedPattern reports the deny-list entry an endpoint path pattern (with
// {param} segments) falls under, or "". Generators use it to leave out
// endpoints the broker would always refuse.
func DeniedPattern(host, pathPattern string) string {
	h, err := CanonicalHost(host)
	if err != nil {
		return ""
	}
	segs := strings.Split(strings.Trim(strings.ToLower(pathPattern), "/"), "/")
	return denied(h, segs)
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
