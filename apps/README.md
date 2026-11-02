# Apps

Three web apps live here. None of them is needed to use the CLI.

| App | What it is | Run locally | Deploys to |
|---|---|---|---|
| [`dashboard/`](dashboard/) | The optional personal dashboard: what your agents made, what's open to anyone, what's waiting on you. Next.js, reads the sync API (`server/`) | `cd apps/dashboard && npm ci && npm run dev` (http://localhost:3100). With no Supabase project, use the dev sign-in described in its README | Railway, as `app.muretto.ai`, next to the API (`server/README.md`) |
| [`site/`](site/) | The muretto.ai landing page. One static page served by a Cloudflare Worker. No build step, no analytics | `cd apps/site && npx wrangler dev` (http://localhost:8787) | Cloudflare Workers, `muretto.ai` (`npx wrangler deploy`) |
| [`docs/`](docs/) | The documentation site, built with Mintlify | `cd apps/docs && npx mint dev` (http://localhost:3000) | Mintlify, through its GitHub app, on push to the default branch |

The dashboard's types in `dashboard/src/contract/` are generated from `../schema` (`npm run generate`); CI checks they are current.
