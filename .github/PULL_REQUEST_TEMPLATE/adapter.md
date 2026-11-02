<!-- Adapter pull request. Guide: adapters/README.md. Format: adapters/WRITING.md.
     Rules for official adapters: adapters/STANDARDS.md. -->

## Provider

<!-- Name, API docs link, and the token type users create. -->

## What it lists and flags

<!-- Kinds (with full / partial / planned) and exposure rules. -->

## Fixtures

- [ ] Hand-built from the provider's documented response shapes and examples
- [ ] Recorded from a test account through `TestRecordLive`, then reviewed by hand

## Checklist

Required (CI checks the ones marked CI):
- [ ] Everything is in `adapters/<provider>/`, and `provider:` matches the folder name (CI)
- [ ] `make adapter-test P=<provider>` passes: `go test ./adapters/` and `muretto adapter test adapters/<provider>/adapter.yaml --strict` (CI)
- [ ] A `basic` scan scenario and at least one failure scenario (403 or 500) (CI)
- [ ] Every stripped field holds `MURETTO_FAKE_SECRET`, listed under `forbidden` (CI)
- [ ] Every signature has an extract case; every `created` signature has a case with `matches:`; one case proves nothing (CI)
- [ ] `research.md` cites a source URL and date for every endpoint, field, token permission and CLI/MCP output string. Nothing invented.
- [ ] `coverage.md` and `coverage:` list every resource kind: `full`, `partial` or `planned`, with the reason
- [ ] GET only. Endpoints that return secrets or write are listed under "Never call" in `research.md`
- [ ] `strip` covers every field that can hold a secret; attributes hold no secrets or personal data
- [ ] `token_help` names the narrowest token, and says plainly what it can do if it is not read-only
- [ ] Every paginated list declares `pagination`, or is `partial` in coverage
- [ ] Rules are three-valued (missing data is `unknown`), and each `caveat` says what the API cannot see
- [ ] Signatures: `created` only when success proves creation; `container` only when the creating call named it
- [ ] Fake values only (`example-org`, `prj_EXAMPLE…`, `@example.com`); no real ids, URLs or tokens

For official adapters (a maintainer can do these):
- [ ] Endpoints that return secrets added to `deniedPaths` in `internal/broker/pinned.go`
- [ ] The provider's token formats added to `internal/redact/redact.go`, with a test
- [ ] `docs/SECURITY.md` notes the Go changes
- [ ] Dashboard provider list updated (`apps/dashboard/src/app/(app)/providers/_components/derive.ts`)
- [ ] README "Supported today" table updated
- [ ] Verified read-only against a live test account, and "Live verification" filled in `research.md`; otherwise the header says `# Not yet verified live.`

## Open questions

<!-- Anything you couldn't confirm from the docs. -->
