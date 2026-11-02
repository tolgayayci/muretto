# Muretto docs

The Muretto documentation site, built with [Mintlify](https://mintlify.com).

| File | What |
|---|---|
| `docs.json` | Site config: name, theme, colors, navigation |
| `style.css` | The Muretto design system mapped onto Mintlify (dark only) |
| `index.mdx` | Introduction |
| `quickstart.mdx` | Install, `muretto init`, first scan |
| `providers.mdx` | Supported providers and exposure checks |
| `how-it-works.mdx` | Hooks, spool, broker, attribution |
| `privacy.mdx` | What leaves the machine; secrets |
| `AGENTS.md` | Writing rules for these pages |

## Preview locally

```sh
cd apps/docs
npx mint dev        # http://localhost:3000
```

## Writing rules

- Every command must exist in `cmd/muretto/commands.go`. Copy CLI output from real runs or from the golden files in `internal/render/testdata/`.
- Muretto shows, lists, traces and flags. Never write that it fixes, cleans up or suggests fixes.
- Use fictional names (`acme-dev`, `customers`, `example-team`), never real project refs, URLs, account ids or keys.

## Publishing

Changes are published by Mintlify's GitHub app when they reach the default branch.
