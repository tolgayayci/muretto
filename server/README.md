# murettod: the Muretto sync API

The optional server behind `muretto login`, `muretto sync` and the web dashboard. Muretto works fully without it. The API contract is the "Sync API" table in `docs/ARCHITECTURE.md`; what the CLI sends is in `docs/SYNC_PAYLOAD.md`.

## Run locally

```sh
# In-memory store + dev login: no database, no GitHub app needed.
MURETTO_DEV_LOGIN=1 go run ./server/cmd/murettod
# listening on 127.0.0.1:8080; the dashboard is expected on http://localhost:3000
```

Sign in as anyone (dev only): open `http://127.0.0.1:8080/auth/dev/login?login=alice&name=Alice&return_to=/`. Get a CLI token without the device flow (demo harness):

```sh
curl -s -X POST -H 'Content-Type: application/json' -d '{"login":"alice"}' http://127.0.0.1:8080/v1/dev/token
```

With Postgres: set `MURETTO_DATABASE_URL`. Migrations run at startup inside one transaction under a Postgres advisory lock, so several instances can start at once.

> ## ⚠️ `MURETTO_DEV_LOGIN=1` is DEV ONLY
> It lets **anyone who can reach the server sign in as any user** and mint CLI tokens with no password. It exists for local dashboard development and the demo. The server **refuses to start** with it unless the listen address is loopback (`127.0.0.1`, `::1` or `localhost`); `:8080` or `$PORT` alone are refused. Never set it on Railway or any shared host.

## Environment variables

| Variable | Default | Meaning |
|---|---|---|
| `MURETTO_ADDR` | `127.0.0.1:8080`, or `:$PORT` if `PORT` is set | Listen address |
| `MURETTO_PUBLIC_URL` | `http://<addr>` | The API's own public base URL; GitHub redirects to `<this>/auth/github/callback`. Must be `https` unless loopback. With `https`, cookies are `Secure` and HSTS is sent |
| `MURETTO_DASHBOARD_URL` | `http://localhost:3000` | The dashboard origin: the **only** CORS origin (with credentials), and where logins, invites (`/join/<token>`) and device approval (`/device?code=`) point. Must be `https` unless loopback |
| `MURETTO_DATABASE_URL` | empty = in-memory store (data lost on restart) | Postgres URL. On Supabase use the **session-mode pooler** (`aws-<n>-<region>.pooler.supabase.com:5432`, user `postgres.<ref>`) |
| `MURETTO_DATABASE_LISTEN_URL` | `MURETTO_DATABASE_URL` | Connection used for `LISTEN`. Must be session mode or direct; port 6543 is refused |
| `MURETTO_DB_MAX_CONNS` | `5` | pgx pool size. Keep it small on the shared pooler |
| `MURETTO_GITHUB_CLIENT_ID`, `MURETTO_GITHUB_CLIENT_SECRET` | empty | GitHub **OAuth App** credentials (set both). Without them dashboard sign-in returns 503 |
| `MURETTO_DEV_LOGIN` | `0` | **DEV ONLY**, loopback only. See above |
| `MURETTO_TRUST_PROXY` | `0` | `1` behind a proxy that appends the client IP to `X-Forwarded-For` (Railway). Used only for per-IP rate limits |
| `MURETTO_MAX_BODY_BYTES` | `8388608` | Ingest body limit (413 above it) |
| `MURETTO_INGEST_PER_MINUTE` | `30` | Ingest rate per CLI token (burst up to 10) |

Secrets come only from the environment. Logs are JSON lines with a request id, the route pattern, status and duration; they never include tokens, cookies, request bodies, query strings or raw paths (invite tokens and OAuth codes live there).

## GitHub OAuth App

Create a GitHub **OAuth App** (not a GitHub App). Callback URL: `<MURETTO_PUBLIC_URL>/auth/github/callback`. No scopes are requested. The server uses PKCE and `state`, reads `GET /user` once, keys the user by GitHub's numeric id and **discards the GitHub token**. The dashboard session is a random token in an `HttpOnly; SameSite=Lax` cookie (`Secure` on https), stored only as a sha256 hash, valid 30 days.

## Deploy on Railway

1. New service from this GitHub repo. In the service settings set the Dockerfile path to `server/Dockerfile` (or the variable `RAILWAY_DOCKERFILE_PATH=server/Dockerfile`). The build context is the repository root.
2. Variables: `MURETTO_PUBLIC_URL=https://api.<domain>`, `MURETTO_DASHBOARD_URL=https://app.<domain>`, `MURETTO_DATABASE_URL=<Supabase session pooler URL, sslmode=require>`, `MURETTO_GITHUB_CLIENT_ID`, `MURETTO_GITHUB_CLIENT_SECRET`, `MURETTO_TRUST_PROXY=1`. Do **not** set `MURETTO_ADDR` (Railway's `PORT` is used) and never set `MURETTO_DEV_LOGIN`.
3. Health check path: `/healthz`.
4. Put the API and dashboard on sibling subdomains of one domain (`api.` and `app.`) so the `SameSite=Lax` cookie is sent on the dashboard's `fetch(..., {credentials: "include"})` and `EventSource(..., {withCredentials: true})`. `*.vercel.app` previews are cross-site and will not get the cookie.
5. Run one instance in v1 (rate limits are in-process). A second instance still gets live events through LISTEN/NOTIFY.

Railway closes idle HTTP requests after 5 minutes and any request after 15; the SSE stream sends a heartbeat every 20 s and `EventSource` reconnects with `Last-Event-ID`.

Build the image locally: `docker build -f server/Dockerfile -t murettod .` (static `CGO_ENABLED=0` binary on `distroless/static:nonroot`).

## Dashboard integration notes (beyond the ARCHITECTURE.md table)

- **CSRF:** every cookie-authenticated `POST`/`DELETE` needs header `X-CSRF-Token: <csrf_token>`, where `csrf_token` comes from `GET /v1/me` (present only for cookie auth). JSON bodies need `Content-Type: application/json`; unknown fields are rejected (400).
- **Login:** link the browser to `GET /auth/github/login?return_to=/some/path` (relative paths only). Success redirects to `<dashboard><return_to>`; failure to `<dashboard>/login?error=state|denied|github|expired`.
- **Device approval page** (`<dashboard>/device?code=XXXX-XXXX`): `GET /v1/device?user_code=XXXX-XXXX` → `{user_code, client: {client, host, os, version}, expires_at}` (404 `invalid_user_code` if unknown/expired). Then `POST /v1/device/approve {user_code, deny?: true}` → `{status: "approved"|"denied"}`.
- **Orgs:** `POST /v1/orgs` and `GET /v1/orgs/{id}` return the `Org` contract type (members carry `developer_id` = server user id, `account_login`, `display_name`, `role`, `joined_at`). Non-members get 404. Invites: `POST /v1/orgs/{id}/invites` → `{url: "<dashboard>/join/mri_…", expires_at}` (7 days, single use, owner only, 403 otherwise). The join page calls `POST /v1/invites/{token}/accept` → `{id, name, role}`; 404 `invalid_invite` if unknown, used or expired.
- **Answers:** both are `Answer`-shaped (`schema/answer.schema.json`). `developer` is the viewer (`id` = server user id, `account_login` = GitHub login). `sessions[].developer_name` is the syncing member's display name. `summary` is recomputed server-side; `warnings` is empty.
- **SSE:** `new EventSource("/v1/orgs/{id}/events", {withCredentials: true})`. Events are default `message` events (use `onmessage`): `id: <seq>`, `data: {"seq", "type", "id", "developer", "at"}`. `type` is `session|resource|attribution|finding|cost|review|check`, or `bulk` (id = batch id) when one upload changed more than 100 entities. `developer` is the member's login. Events carry ids only: refetch `GET /v1/orgs/{id}/answer` (debounced). Replay on reconnect covers up to 1000 events.
- **Logout:** `POST /auth/logout` (CSRF) → 204. **CLI logout:** `DELETE /v1/tokens/self` with the bearer token → 204.
- **Errors** are `{error, message?, problems?, request_id}`; 429 responses carry `Retry-After`. Provider requests: `POST /v1/provider-requests {provider, email?}` → 202 `{status: "received"}`, limited per IP (3 burst, 5/hour).

## Tests

`go test ./server/...` runs everything against the in-memory store, with no network. Postgres tests are opt-in: `MURETTO_PG_URL=postgres://… go test ./server/internal/store/pgstore` (use a disposable database).
