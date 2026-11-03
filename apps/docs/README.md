# Muretto docs

The Muretto documentation site, built with [Mintlify](https://mintlify.com).

| File | What |
|---|---|
| `docs.json` | Site config: name, theme, colors, navigation |
| `style.css` | The Muretto design system mapped onto Mintlify (dark only) |
| `index.mdx`, `quickstart.mdx`, `how-it-works.mdx` | Get started |
| `examples.mdx`, `dashboard.mdx` | Guides |
| `providers.mdx`, `adapters.mdx` | Providers and writing an adapter |
| `cli.mdx`, `privacy.mdx` | Reference |
| `images/` | Dashboard screenshots, taken from the dashboard's sample data |

## Preview locally

```sh
cd apps/docs
npx mint@4.2.1063 dev    # http://localhost:3000
```

## Writing rules

- Every command must exist in `cmd/muretto/commands.go`. Copy CLI output from real runs or from the golden files in `internal/render/testdata/`.
- Muretto shows, lists, traces and flags. Never write that it changes, cleans up or suggests changes to anything.
- Use fictional names (`acme-dev`, `customers`, `example-team`), never real project refs, URLs, account ids or keys.

## Publishing

Changes are published by Mintlify's GitHub app when they reach the default branch.
