# Adapter fixtures

Everything here is **fake**. No Muretto test account existed when these files
were written (2026-10-26), so every response was built by hand from the
documented response shapes and examples in `adapters/supabase/research.md`,
`adapters/vercel/research.md` and the providers' OpenAPI specs cited there. None
of it was recorded from a real account.

Fake values used throughout:

| What | Fake |
|---|---|
| Supabase project refs | `abcdefghijklmnopqrst`, `bcdefghijklmnopqrstu`; branch `tsrqponmlkjihgfedcba` |
| Supabase org | `example-org` |
| Vercel ids | `prj_EXAMPLE…`, `dpl_EXAMPLE…`, `team_EXAMPLE…`, team slug `example-team` |
| Emails | `@example.com` only |
| Secret values the broker must strip | contain `MURETTO_FAKE_SECRET`; scan tests fail if that marker reaches a `ScanResult` |

`TestFixturesHaveNoSecrets` fails if any file under `adapters/` matches a
secret pattern (JWTs, Supabase personal access tokens and secret keys, Vercel and GitHub
tokens, private keys, URLs with credentials, or emails outside example.com).

## Layout

```
<provider>/scan/<scenario>/routes.yaml   requests the fake API answers (method, path, query subset, status, file|body)
<provider>/scan/<scenario>/expect.yaml   account, options, expected resources, fields, findings, fixes, checks, forbidden strings
<provider>/scan/<scenario>/*.json        response bodies (raw provider shape, before broker stripping)
<provider>/extract/*.yaml                Extract cases: tool, input, output, status, expected identifiers
```

`internal/adapters` runs every scenario against an `httptest` server through
the real broker, re-checks every request that reached the server against the
allowlist, and runs every extract case. A new provider needs only new
directories here: no Go changes.

Scenarios today:

- `supabase/scan/basic`: project, branch, function, a public and a private bucket, an RLS-exposed table from the security advisor.
- `supabase/scan/failures`: 403, 500, 422 (branching off) and an `advisor_check_unavailable` lint, which must become error checks (so answers say unknown).
- `supabase/scan/catalog`: the opt-in `catalog_rls` read-only catalog query, combined with exposed schemas.
- `vercel/scan/basic`: paginated projects with protection off / Standard / absent, preview and production deployments, domains, env vars (values stripped), a 403 env list and the bare-object env response.

## Recording real responses (later)

When test accounts exist, record through the broker (never around it):

```
MURETTO_LIVE=1 MURETTO_SUPABASE_TOKEN=... MURETTO_VERCEL_TOKEN=... \
  go test ./internal/adapters -run TestRecordLive -v
```

Optional: `MURETTO_SUPABASE_ACCOUNT` / `MURETTO_VERCEL_ACCOUNT` (id or slug)
and `MURETTO_RECORD_OPTIONS=catalog_rls`. The recorder replaces the values of
the keys listed under `recorder.pseudonymize` in each adapter with stable fakes
(same input, same fake, also inside paths and URLs), refuses to write anything
that still matches a secret pattern, and writes to
`<provider>/recorded/<timestamp>/` (git-ignored). Review the files by hand,
add an `expect.yaml`, and move them under `scan/` before committing. Only use
test accounts.
