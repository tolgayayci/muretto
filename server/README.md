# murettod: the Muretto sync API

The optional server behind `muretto login`, `muretto sync` and the web dashboard. Muretto works fully without it. The API contract is the "Sync API" table in `docs/ARCHITECTURE.md`; what the CLI sends is in `docs/SYNC_PAYLOAD.md`.

## Run locally

```sh
# In-memory store + dev login: no database, no Supabase project needed.
MURETTO_DEV_LOGIN=1 go run ./server/cmd/murettod
# listening on 127.0.0.1:8080; the dashboard is expected on http://localhost:3000
```

Get a token for anyone without the device flow (dev only; demo harness):

```sh
curl -s -X POST -H 'Content-Type: application/json' -d '{"login":"alice"}' http://127.0.0.1:8080/v1/dev/token
```

With dev login on, that `mrt_` token is also accepted where a dashboard user is required (device approval, token list/revoke, orgs), so local development and the demo run without Supabase. To test real dashboard sign-in locally, also set `MURETTO_SUPABASE_URL` (a hosted project, or `http://127.0.0.1:54321` for the Supabase CLI's local stack).

With Postgres: set `MURETTO_DATABASE_URL`. Migrations run at startup inside one transaction under a Postgres advisory lock, so several instances can start at once.

> ## ⚠️ `MURETTO_DEV_LOGIN=1` is DEV ONLY
> It lets **anyone who can reach the server sign in as any user** and mint CLI tokens with no password, and lets CLI tokens act as dashboard users. It exists for local dashboard development and the demo. The server **refuses to start** with it unless the listen address is loopback (`127.0.0.1`, `::1` or `localhost`); `:8080` or `$PORT` alone are refused. Never set it on Railway or any shared host.

## Environment variables

| Variable | Default | Meaning |
|---|---|---|
| `MURETTO_ADDR` | `127.0.0.1:8080`, or `:$PORT` if `PORT` is set | Listen address |
| `MURETTO_PUBLIC_URL` | `http://<addr>` | The API's own public base URL. Must be `https` unless loopback. With `https`, HSTS is sent |
| `MURETTO_DASHBOARD_URL` | `http://localhost:3000` | The dashboard origin: the **only** CORS origin (never with credentials), and where invites (`/join/<token>`) and device approval (`/device?code=`) point. Must be `https` unless loopback |
| `MURETTO_SUPABASE_URL` | empty | The Supabase project URL, `https://<ref>.supabase.co` (no path; `https` unless loopback). Access tokens must have issuer `<this>/auth/v1`; keys come from `<this>/auth/v1/.well-known/jwks.json`. **Required** unless `MURETTO_DEV_LOGIN=1` |
| `MURETTO_SUPABASE_JWT_SECRET` | empty | Optional legacy JWT secret (at least 32 characters). Only when set are HS256 tokens accepted; leave it empty for projects on asymmetric signing keys |
| `MURETTO_DATABASE_URL` | empty = in-memory store (data lost on restart) | Postgres URL. On Supabase use the **session-mode pooler** (`aws-<n>-<region>.pooler.supabase.com:5432`, user `postgres.<ref>`) |
| `MURETTO_DATABASE_LISTEN_URL` | `MURETTO_DATABASE_URL` | Connection used for `LISTEN`. Must be session mode or direct; port 6543 is refused |
| `MURETTO_DB_MAX_CONNS` | `5` | pgx pool size. Keep it small on the shared pooler |
| `MURETTO_DEV_LOGIN` | `0` | **DEV ONLY**, loopback only. See above |
| `MURETTO_TRUST_PROXY` | `0` | `1` behind a proxy that appends the client IP to `X-Forwarded-For` (Railway). Used only for per-IP rate limits |
| `MURETTO_MAX_BODY_BYTES` | `8388608` | Ingest body limit (413 above it) |
| `MURETTO_INGEST_PER_MINUTE` | `30` | Ingest rate per CLI token (burst up to 10) |

`MURETTO_GITHUB_CLIENT_ID` / `MURETTO_GITHUB_CLIENT_SECRET` are no longer used (the server logs a warning if they are set).

Secrets come only from the environment. Logs are JSON lines with a request id, the route pattern, status and duration; they never include tokens, request bodies, query strings or raw paths (invite tokens and user codes live there).

## Authentication

Every credential is a Bearer token: `Authorization: Bearer <token>`. There are no cookies, so there is no CSRF token.

- **Dashboard users** sign in with Supabase Auth (GitHub provider) in `apps/dashboard`. The dashboard's server code (never the browser) forwards the user's Supabase access token. murettod verifies it itself: ES256 or RS256 against the project's JWKS (the `kid` must name a key; HS256 only with `MURETTO_SUPABASE_JWT_SECRET`; `none` and everything else refused), `iss` = `<MURETTO_SUPABASE_URL>/auth/v1` exactly, `aud` contains `authenticated`, `exp` required, `nbf`/`iat` checked, 60 s leeway, `sub` a UUID, `role` = `authenticated`, anonymous users refused. The JWKS is cached for 10 minutes and refetched on an unknown `kid` at most once a minute. The account is `supabase:<sub>`. Login and display name come from `user_metadata` (`user_name`/`preferred_username`, `full_name`/`name`), which users can edit, so they are display-only and never used for identity or authorization. Code: `server/internal/supaauth`.
- **CLI tokens** (`mrt_…`) come from the device flow: random, stored as sha256, revocable, `last_used_at` updated at most once a minute.

| Routes | Accepts |
|---|---|
| `POST /v1/device/code`, `POST /v1/device/token`, `POST /v1/provider-requests`, `GET /healthz` | nothing (rate-limited per IP); a provider request with a dashboard sign-in is limited per user and records who asked |
| `POST /v1/ingest`, `DELETE /v1/tokens/self` | CLI token only |
| `GET /v1/me`, `GET /v1/answer`, `GET /v1/reviews` | CLI token or Supabase token |
| `GET /v1/device`, `POST /v1/device/approve`, `POST /v1/reviews`, `GET /v1/tokens`, `DELETE /v1/tokens/{id}`, `POST /v1/orgs`, `GET /v1/orgs/{id}`, `POST /v1/orgs/{id}/invites`, `POST /v1/invites/{token}/accept`, `GET /v1/orgs/{id}/answer`, `GET /v1/orgs/{id}/events` | Supabase token (403 `dashboard_auth_required` for a CLI token, so a stolen CLI token cannot approve new device logins or manage tokens). With `MURETTO_DEV_LOGIN=1` a CLI token is accepted too |
| `POST /v1/dev/token` | dev login only; not registered otherwise |

Supabase project setup: enable the GitHub provider and add `<dashboard>/auth/callback` to the redirect URLs. Disable anonymous sign-ins (their tokens are refused anyway).

## Deploy on Railway

1. New service from this GitHub repo. In the service settings set the Dockerfile path to `server/Dockerfile` (or the variable `RAILWAY_DOCKERFILE_PATH=server/Dockerfile`). The build context is the repository root.
2. Variables: `MURETTO_PUBLIC_URL=https://api.<domain>`, `MURETTO_DASHBOARD_URL=https://app.<domain>`, `MURETTO_DATABASE_URL=<Supabase session pooler URL, sslmode=require>`, `MURETTO_SUPABASE_URL=https://<ref>.supabase.co`, `MURETTO_TRUST_PROXY=1`. Do **not** set `MURETTO_ADDR` (Railway's `PORT` is used) and never set `MURETTO_DEV_LOGIN`.
3. Health check path: `/healthz`.
4. The dashboard calls the API from its server with Bearer tokens, so the API and dashboard need not share a domain (`*.vercel.app` previews work), but only `MURETTO_DASHBOARD_URL` is a CORS origin.
5. Run one instance in v1 (rate limits are in-process). A second instance still gets live events through LISTEN/NOTIFY.

Railway closes idle HTTP requests after 5 minutes and any request after 15; the SSE stream sends a heartbeat every 20 s and `EventSource` reconnects with `Last-Event-ID`.

Build the image locally: `docker build -f server/Dockerfile -t murettod .` (static `CGO_ENABLED=0` binary on `distroless/static:nonroot`).

## Dashboard integration notes (beyond the ARCHITECTURE.md table)

- **Requests:** send `Authorization: Bearer <Supabase access token>` from server code. JSON bodies need `Content-Type: application/json`; unknown fields are rejected (400). A 401 means the token is missing, invalid or expired: refresh the Supabase session and retry once.
- **Me:** `GET /v1/me` → `{login, display_name, orgs: [{id, name, role}]}`.
- **Device approval page** (`<dashboard>/device?code=XXXX-XXXX`): `GET /v1/device?user_code=XXXX-XXXX` → `{user_code, client: {client, host, os, version}, expires_at}` (404 `invalid_user_code` if unknown/expired). Then `POST /v1/device/approve {user_code, deny?: true}` → `{status: "approved"|"denied"}`.
- **Orgs:** `POST /v1/orgs` and `GET /v1/orgs/{id}` return the `Org` contract type (members carry `developer_id` = server user id, `account_login`, `display_name`, `role`, `joined_at`). Non-members get 404. Invites: `POST /v1/orgs/{id}/invites` → `{url: "<dashboard>/join/mri_…", expires_at}` (7 days, single use, owner only, 403 otherwise). The join page calls `POST /v1/invites/{token}/accept` → `{id, name, role}`; 404 `invalid_invite` if unknown, used or expired.
- **Answers:** both are `Answer`-shaped (`schema/answer.schema.json`). `developer` is the viewer (`id` = server user id, `account_login` = the login from Supabase `user_metadata`, display-only). `sessions[].developer_name` is the syncing member's display name. `summary` is recomputed server-side; `warnings` is empty. In the team answer, copies of one entity from several members are merged so the view never looks safer than any member's latest report: an exposure stays until the member who reported it syncs a newer check, and two members attributing a resource to different sessions give `unknown` with both evidences (`docs/decisions/0505-team-view-merge.md`).
- **SSE:** `GET /v1/orgs/{id}/events` with the Bearer token (proxy it from the dashboard's server; browsers' `EventSource` cannot set headers). Events are default `message` events (use `onmessage`): `id: <seq>`, `data: {"seq", "type", "id", "developer", "at"}`. `type` is `session|resource|attribution|finding|cost|review|check`, or `bulk` (id = batch id) when one upload changed more than 100 entities. `developer` is the member's login. Events carry ids only: refetch `GET /v1/orgs/{id}/answer` (debounced). Replay on reconnect covers up to 1000 events.
- **Machines (CLI tokens):** `GET /v1/tokens` → `{tokens: [{id, name, created_at, last_used_at}]}`, the caller's active tokens, newest first; `name` is the CLI's host, `last_used_at` is `null` until first use and at most about a minute stale. Hashes are never returned. `DELETE /v1/tokens/{id}` → 204; 404 `not_found` for unknown, already revoked or another user's token.
- **Review decisions:** `POST /v1/reviews {resource_id, state: "keep"|"not_needed"|"pending", note?}` → 204. `resource_id` is a Muretto resource id (`<provider>.<kind>:<provider_id>`, at most 512 characters, no whitespace); `note` at most 500 printable characters (line breaks allowed); `pending` undoes a decision. Rate-limited per user (burst 30, 120/min). Decisions are stored per user and merged into that user's synced review items in `GET /v1/answer` and the team answer: the later of the CLI's `decided_at` (capped at when the server received it) and the dashboard decision wins; a dashboard win sets `decided_by` to the server user id, and an undo shows `pending` with no `decided_at`/`decided_by`/`note`. `summary.needs_review` follows. A decision for a resource without a synced review item is stored but has no effect, and in the team answer a member's decision never hides another member's pending review. `GET /v1/reviews` → `{decisions: [{resource_id, state, note?, decided_at}]}`, the caller's own.
- **Sign-out** is Supabase's (`supabase.auth.signOut()`); the API keeps no session. **CLI logout:** `DELETE /v1/tokens/self` with the CLI token → 204.
- **Errors** are `{error, message?, problems?, request_id}`; 429 responses carry `Retry-After`. Provider requests: `POST /v1/provider-requests {provider, email?}` → 202 `{status: "received"}`, stored in the `provider_requests` table. Anonymous (landing page): limited per IP (3 burst, 5/hour). With a dashboard sign-in (the Providers page): limited per user with the same numbers, and `user_id` records who asked; the per-IP limit can't apply there because every dashboard request comes from the dashboard server's one address.

## Tests

`go test ./server/...` runs everything against the in-memory store, with no network: `server/testserver` runs a fake Supabase project (an ES256 key and its JWKS on a loopback `httptest` server) and signs real access tokens. Postgres tests are opt-in: `MURETTO_PG_URL=postgres://… go test ./server/internal/store/pgstore` (use a disposable database).
