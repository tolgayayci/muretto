# Adapter fixtures

Every fixture under `adapters/<provider>/testdata/` is **fake**. The fixtures
for Supabase and Vercel (2026-10-26) and for GitHub, Cloudflare, Neon, Stripe,
Render and PlanetScale (2026-11-01) were built by hand from the documented
response shapes and examples cited in each provider's `research.md` and the
providers' OpenAPI specs. None of them were recorded from a real account.

That is also the expected path for a community pull request: build the
responses from the provider's docs, use the fake values below, and say in your
`research.md` that the fixtures are hand-built.

Fake values used throughout:

| What | Fake |
|---|---|
| Supabase project refs | `abcdefghijklmnopqrst`, `bcdefghijklmnopqrstu`; branch `tsrqponmlkjihgfedcba` |
| Org, team and account slugs | `example-org`, `example-team`, `octo-example` (GitHub) |
| Prefixed ids | the provider's prefix plus `EXAMPLE`: `prj_EXAMPLE…`, `dpl_EXAMPLE…`, `team_EXAMPLE…` (Vercel), `acct_EXAMPLE…` (Stripe); `tea-example…` (Render) |
| Hex ids | obvious patterns such as `0123456789abcdef0123456789abcdef` (Cloudflare) |
| Resource names | plain, made-up names such as `customers`, `waitlist-preview` |
| Emails | `@example.com` only |
| Secret values the broker must strip | contain `MURETTO_FAKE_SECRET`; scan tests fail if that marker reaches a `ScanResult` |

`TestFixturesHaveNoSecrets` (in `internal/adapters`) fails if any file under
`adapters/` matches a secret pattern (JWTs, Supabase personal access tokens and
secret keys, Vercel and GitHub tokens, private keys, URLs with credentials, or
emails outside example.com).

## Layout

```
adapters/<provider>/testdata/scan/<scenario>/routes.yaml   requests the fake API answers (method, path, query subset, status, file|body)
adapters/<provider>/testdata/scan/<scenario>/expect.yaml   account, options, expected resources, fields, findings, checks, forbidden strings
adapters/<provider>/testdata/scan/<scenario>/*.json        response bodies (raw provider shape, before broker stripping)
adapters/<provider>/testdata/extract/cases.yaml            extract cases: tool, input, output, status, expected identifiers
adapters/<provider>/testdata/recorded/                     output of the live recorder (git-ignored)
```

`go test ./adapters/` and `internal/adapters` run every scenario against an
`httptest` server through the real broker, re-check every request that
reached the server against the allowlist, and run every extract case. The
format of each file is in [WRITING.md section 9](WRITING.md#9-fixtures-and-tests).

Scenarios for the first two adapters, as examples of what to cover:

- `supabase/testdata/scan/basic`: project, branch, function, a public and a private bucket, an RLS-exposed table from the security advisor.
- `supabase/testdata/scan/failures`: 403, 500, 422 (branching off) and an `advisor_check_unavailable` lint, which must become error checks (so answers say unknown).
- `supabase/testdata/scan/catalog`: the opt-in `catalog_rls` read-only catalog query, combined with exposed schemas.
- `vercel/testdata/scan/basic`: paginated projects with protection off / Standard / absent, preview and production deployments, domains, env vars (values stripped), a 403 env list and the bare-object env response.

Every other provider has at least `basic` and `failures`; some add a
pagination scenario.

## Recording real responses

Only with **test** accounts, and only through the broker (never around it):

```
MURETTO_LIVE=1 MURETTO_SUPABASE_TOKEN=... MURETTO_VERCEL_TOKEN=... \
  go test ./internal/adapters -run TestRecordLive -v
```

It records every provider whose `MURETTO_<PROVIDER>_TOKEN` is set.
Optional: `MURETTO_<PROVIDER>_ACCOUNT` (id or slug) and
`MURETTO_RECORD_OPTIONS=catalog_rls`. The recorder replaces the values of
the keys listed under `recorder.pseudonymize` in each adapter with stable fakes
(same input, same fake, also inside paths and URLs), refuses to write anything
that still matches a secret pattern, and writes to
`adapters/<provider>/testdata/recorded/<timestamp>/` (git-ignored). Review the
files by hand, add an `expect.yaml`, and move them under `scan/` before
committing.
