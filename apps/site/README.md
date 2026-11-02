# muretto.ai landing page

A single static page served by a Cloudflare Worker with static assets. There's no build step and no analytics.

```
public/          what gets served (index.html, 404.html, og.png, favicon, robots, sitemap)
src/worker.js    redirects http and www to https://muretto.ai; adds security headers
                 and Cache-Control no-transform (stops Cloudflare injecting its analytics beacon)
wrangler.jsonc   Worker config; custom domains muretto.ai and www.muretto.ai
```

## Edit

`public/index.html` is the whole page. All links, install commands and the hidden founder section are driven by the `CONFIG` object near the top of its `<script>`. An empty value hides its link.

## Run locally

```sh
npm ci
npm run dev             # http://localhost:8787
npm run check           # dry-run build into dist/, no deploy
```

## Deploy

Deploys need a login to the Cloudflare account that owns the muretto.ai zone:

```sh
npx wrangler login
CLOUDFLARE_ACCOUNT_ID=<account id> npm run deploy
```

In CI, set `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` instead of logging in.
