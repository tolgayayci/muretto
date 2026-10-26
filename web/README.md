# Muretto web: dashboard and landing page

Next.js 16 app. The landing page is `/`; the dashboard is `/app`.

## Run

```sh
npm ci
npm run dev                 # http://localhost:3000
```

Without a server, open **`/app/sample`**: the full dashboard on built-in, clearly labelled sample data (the fake "acme" team). Set `NEXT_PUBLIC_MURETTO_SAMPLE=1` to make `/app` use it too.

With the Go sync API (see `server/README.md`):

```sh
MURETTO_DEV_LOGIN=1 go run ./server/cmd/murettod     # from the repo root: 127.0.0.1:8080
cd web && MURETTO_API_URL=http://127.0.0.1:8080 npm run dev
```

Then use the "Dev sign-in" form on `/login` (development builds only).

## Routes

| Route | What |
|---|---|
| `/` | Landing page |
| `/app` | Dashboard (live, Me / Team) |
| `/app/sample` | Dashboard on sample data |
| `/app/orgs/new` | Create a team |
| `/login` | GitHub sign-in (`?return_to=`, `?error=`) |
| `/device?code=XXXX-XXXX` | Approve or deny a `muretto login` |
| `/join/<token>` | Accept a team invite |

## Scripts

`npm run lint`, `npx tsc --noEmit`, `npm run build`, and `npm run generate` (TypeScript types in `src/contract/` from `../schema/*.schema.json`; commit the output).

## Configuration

See `.env.example`. Decisions: `docs/decisions/0601`–`0604`.
