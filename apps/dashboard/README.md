# Muretto dashboard

The personal dashboard: what your coding agents made, what's open to anyone, and what's waiting on you. Next.js 16 (App Router, Server Components), React 19, TypeScript. It reads one Answer from the sync API (`murettod`, in `server/`) and never calls that API from the browser.

## Run it locally

```sh
cd apps/dashboard
npm ci
cp .env.example .env.local   # then fill in the values below
npm run dev                  # http://localhost:3100
```

Without a Supabase project, use the development sign-in (dev builds only):

```sh
# terminal 1: the sync API, in memory, with the dev-only login
MURETTO_DEV_LOGIN=1 MURETTO_ADDR=127.0.0.1:8080 MURETTO_DASHBOARD_URL=http://localhost:3100 go run ./server/cmd/murettod

# terminal 2
MURETTO_DEV_AUTH=1 MURETTO_API_URL=http://127.0.0.1:8080 npm run dev
```

On `/login`, "Sign in to the local API as alice" signs in through the dev API, and the `sample: …` links browse built-in sample data for every state (normal, new, waiting, clear, many, problem) without any API.

## Sign-in with Supabase Auth (GitHub)

1. In your Supabase project: **Authentication → Sign In / Providers → GitHub**: enable it and paste the client id and secret of a GitHub OAuth app whose callback URL is `https://<project-ref>.supabase.co/auth/v1/callback`.
2. **Authentication → URL Configuration**: set the Site URL to the dashboard's origin (Supabase always accepts a return address on that host) and add `http://localhost:3100/**` to the redirect URLs for local work. The return address carries `?next=`, and Supabase matches allow-list entries against the whole URL, so an exact `/auth/callback` entry never matches; use `/**`.
3. Dashboard env: `NEXT_PUBLIC_SUPABASE_URL` and `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY` (the publishable key, never the secret or service-role key).
4. Server env: `MURETTO_SUPABASE_URL` (same project URL). murettod verifies every access token against the project's JWKS. Use asymmetric JWT signing keys (the default for new projects); `MURETTO_SUPABASE_JWT_SECRET` exists only for legacy HS256 projects.
5. Set `MURETTO_DASHBOARD_URL` on the server to the dashboard's origin: `muretto login` sends people to `<dashboard>/device?code=…`.

The GitHub sign-in asks only who you are (no repository access). Supabase stores the session in cookies; the dashboard's proxy refreshes it and verifies it with `getClaims()` before rendering any page.

## Deploy on Railway

The dashboard runs as its own Railway service, next to the API (`server/README.md`) and a Railway Postgres, in one project.

1. Build from `apps/dashboard` with its `Dockerfile` (`next build`, then `next start` on `PORT`, non-root).
2. Variables: `NEXT_PUBLIC_SUPABASE_URL`, `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY` (both inlined at build time, passed in as build args), `PORT=3000`, and `MURETTO_API_URL=http://${{api.RAILWAY_PRIVATE_DOMAIN}}:8080` so the dashboard reaches the API over Railway's private network.
3. Health check path: `/login`. Custom domain `app.<domain>` (a CNAME plus Railway's `_railway-verify` TXT record).
4. Set the Supabase Site URL to `https://app.<domain>` (or add `https://app.<domain>/**` to the redirect URLs), or GitHub sign-in falls back to the Site URL.

Behind Railway's proxy a route handler sees its own address as `localhost:3000`, so redirects use relative `Location` headers and the sign-out origin check compares against `Host` (`src/lib/http.ts`).

## Scripts

| Command | What it does |
|---|---|
| `npm run dev` | dev server on :3100 |
| `npm run build` / `npm start` | production build and server |
| `npm run lint`, `npm run typecheck`, `npm test` | ESLint, TypeScript, Vitest |
| `npm run generate` | regenerate `src/contract/` from `../../schema` (CI checks it is current) |

## Layout

- `src/app/login`, `src/app/device`, `src/app/auth/*`: sign-in, CLI device approval, OAuth callback and sign-out.
- `src/app/(app)/*`: the signed-in pages (Overview, My resources, sessions, Providers, Settings), each with its loading and error states.
- `src/lib/model.ts`: turns the Answer into what the pages show. `src/lib/sample.ts`: sample Answers per state.
- `src/lib/api.ts` (server-only): calls murettod with the user's Supabase access token.
- `src/proxy.ts`: per-request nonce CSP, session refresh, sign-in redirect.
- `src/components`: the Muretto design system (`ds.tsx`), provider logos and badges (`brand.tsx`), shared page pieces (`ui.tsx`).

See `docs/decisions/0605-dashboard-v2-supabase-auth.md` for why it is built this way.
