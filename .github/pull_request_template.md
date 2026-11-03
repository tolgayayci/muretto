<!-- Adding or changing a provider adapter? Use the adapter template instead:
     add ?template=adapter.md to this page's URL. -->

## What this changes

## Why

## Checklist
- [ ] `make test` passes (no network, no accounts)
- [ ] Dashboard changes: `npm run lint`, `npm run typecheck`, `npm test` and `npm run build` pass in `apps/dashboard`
- [ ] New behaviour has a test, and no test calls a real provider
- [ ] No secrets, real account ids, URLs or personal data in code, fixtures, docs or screenshots
- [ ] Provider calls go only through `internal/broker`, and only to allowlisted read endpoints
- [ ] Security model changed (broker, redaction, hooks, sync): explained below
- [ ] Schema changed: `make generate` run and the generated files committed
- [ ] New dependencies (if any) are justified below

## New dependencies
