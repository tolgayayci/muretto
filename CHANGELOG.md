# Changelog

All notable changes to this project are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-11-03

The first public version. It ships on npm first; Homebrew and `install.sh` follow with the GitHub release.

### Added

**Recording what agents do**
- Claude Code hooks (`SessionStart`, `UserPromptSubmit`, `PostToolUse`, `PostToolUseFailure`, `SubagentStart`, `SessionEnd`) that write one redacted, size-capped spool file per event and never block the agent.
- Additive, order-preserving install into Claude Code settings with a backup, and an uninstall that removes exactly what was added. The same hooks ship as a plugin directory (`plugin/`).
- Transcript scan as a secondary source: backfill, session metadata and a coverage check. Forked sessions are deduplicated by `tool_use_id`.
- Recording of the project a pinned MCP server points at (for example a Supabase server with `?project_ref=`), and only that id.
- Secret redaction with anchored rules, applied in the hook, at ingest and again on output and sync.

**Reading providers, read-only**
- A broker that is the only provider HTTP client: an allowlist checked before any network I/O, GET only (plus one pinned, read-only catalog query), a deny list for secret-returning endpoints, secret fields stripped from responses, and a test that forbids HTTP clients elsewhere.
- A declarative YAML adapter format with pagination, per-endpoint headers, age rules and three-valued exposure predicates.
- Adapters for Supabase and Vercel (checked against live test accounts), and for GitHub, Cloudflare, Neon, Stripe, Render and PlanetScale (built from each provider's documented API and tested offline).
- `muretto adapter new` drafts an adapter from an OpenAPI 2.0, 3.0, 3.1 or 3.2 file; `adapter check`, `install`, `list` and `remove` manage local adapters. Every built-in adapter must pass its own fixtures offline.

**Attribution and output**
- Attribution that links a resource to a session only when that session's own tool call proves it created it (id in the result, the deployment URL it printed, or a unique name in the container the call named). Everything else is "creator unknown".
- Commands: summary, `sessions`, `session`, `resources`, `show`, `why`, `exposed`, `review`, `providers --coverage`, `doctor`, `init`, `uninstall`, `version`. Every read command takes `--json`, which is always a valid Answer.
- SQLite store with scan history, so a deleted or recreated resource is detected.

**Optional sync and dashboard**
- `muretto login` (device flow) and `muretto sync on|off|status|now`. Sync is off by default and `sync on` prints every field it will send ([`docs/SYNC_PAYLOAD.md`](docs/SYNC_PAYLOAD.md)).
- Sync API (`server/`, `murettod`): schema-validated ingest, hashed CLI tokens, Supabase Auth for dashboard users, review decisions, live events over SSE, a conservative team-view merge in the API, and a loopback-only development login.
- Personal dashboard (`apps/dashboard`) with Supabase Auth sign-in. Team features are marked "soon".
- The muretto.ai landing page (`apps/site`) and a Mintlify docs site (`apps/docs`).

**Distribution and tooling**
- GoReleaser config, a tag-only release workflow, npm packages with per-platform optional dependencies and trusted publishing, a Homebrew cask, and a checksum-verifying `install.sh`. None of these have published anything yet.
- CI with tests, lint, `govulncheck` and a secret scan over the full history.
- An end-to-end test of hooks, ingest, scan, attribution and output.
- An optional live demo (`examples/live-demo`) that runs five headless Claude Code sessions against your own test accounts.

### Removed

- Removed: `muretto fix`; Muretto only shows and flags.
