## What this changes

## Checklist
- [ ] `make test` passes (no network, no accounts)
- [ ] New or changed adapter: `muretto adapter test adapters/<provider>/adapter.yaml --strict` passes, and its fixtures come from recorded, sanitized responses
- [ ] No secrets, real account ids or personal data in code, fixtures, docs or screenshots
- [ ] Provider calls go only through `internal/broker`, and only to allowlisted read endpoints
- [ ] New dependencies (if any) are justified below
- [ ] `docs/SECURITY.md` updated if the security model changed
- [ ] Decision recorded in `docs/DECISIONS.md` if it's non-obvious

## New dependencies
