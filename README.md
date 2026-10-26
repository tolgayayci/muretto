# Muretto

**See everything your AI agents create outside the repo.**

Coding agents like Claude Code now create real infrastructure: databases, tables, storage buckets, functions, deployments. Git tracks the code; nothing tracks what the agents created outside the repo. Muretto is a git log for infrastructure. It shows which agent session created every database, bucket and deployment, what's exposed to the internet, and the exact command to fix it. It is **read-only**: it never touches your infrastructure.

```
Session 8f2a · alice · Claude Code · today 14:02–14:31
Task: "add customer import with file uploads"
Repo: acme/web (feat/import)

  SUPABASE  project acme-dev
  ✓ table      customers        exact   ⚠ RLS off: publicly readable
  ✓ bucket     customer-files   exact   ⚠ public
  ✓ function   process-import   exact

  VERCEL  project acme-web
  ✓ deployment preview-a81f     exact   ⚠ public preview

  3 exposed · checked 2 min ago
  → muretto why customers   → muretto fix customers
```

## Install

```sh
npx muretto init
```

Or install it permanently, which is better for Claude Code hooks:

```sh
npm i -g muretto                                   # npm
brew install tolgayayci/tap/muretto                # Homebrew
curl -fsSL https://raw.githubusercontent.com/tolgayayci/muretto/main/install.sh | sh   # verifies checksums
```

macOS and Linux, arm64 and amd64. Details: [`docs/INSTALL.md`](docs/INSTALL.md).

## Use

```sh
muretto init        # add Claude Code hooks (backed up, removable) and connect Supabase / Vercel
muretto             # summary: what agents created, what still exists, what's exposed
muretto sessions    # recent agent sessions
muretto session 8f2a        # everything one session created
muretto resources --unknown # resources no agent session claims ("creator unknown")
muretto exposed     # exposure findings, worst first
muretto why customers       # the evidence behind an attribution
muretto fix customers       # the command or SQL to fix it (printed, never run)
muretto doctor      # is everything wired up? prints fixes
muretto uninstall   # removes exactly what init added
```

Every read command takes `--json`.

## How it works

1. **The story.** A tiny Claude Code hook writes one redacted file per tool call and exits in a few milliseconds. Muretto also reads Claude Code transcripts to fill gaps.
2. **The truth.** Muretto lists what actually exists in your Supabase and Vercel accounts through read-only API calls.
3. **The join.** A resource is attributed to a session only when that session's own tool call created it: the provider id appears in its output, or a unique name appears in the call that targeted that project. Everything else is labelled **creator unknown**. Muretto never guesses, and `muretto why` shows the evidence for every label.

## Security

- **Read-only, enforced in code.** All provider requests go through one module that allows only an explicit list of read endpoints and refuses everything else before it reaches the network. Tests prove it.
- **No actions.** Fixes are printed for you to run.
- **Secrets are redacted** when recorded and again at output. Tokens live in your OS keychain. Env var values are never stored.
- **Local-first.** Everything works without an account. Sync to the team dashboard is opt-in and shows exactly what it sends first.
- **No telemetry.**

Read the full model, including what your tokens can do, in [`docs/SECURITY.md`](docs/SECURITY.md).

## Supported today

| Provider | Covered | Planned |
|---|---|---|
| **Supabase** | projects, branches, edge functions, storage buckets; tables (agent-created and advisor-flagged, or all of them with an opt-in) | schemas, views, secrets (names), custom domains, read replicas, queues, cron jobs, webhooks, vector buckets |
| **Vercel** | projects, deployments (preview URLs), domains, env var names | aliases, custom environments, Global Config, sandboxes, webhooks, drains |

Exposure checks: tables readable with the public key (via Supabase's own security advisor), public buckets, public preview deployments, and client-exposed env vars that look secret. Run `muretto providers --coverage` for the live matrix.

Agents: Claude Code.

## Write an adapter

Every provider is a declarative, read-only YAML file in [`adapters/`](adapters/). Adding one needs no Go: one YAML file, sanitized fixtures and test cases. Start with [`docs/WRITING_AN_ADAPTER.md`](docs/WRITING_AN_ADAPTER.md) and [`adapters/template.yaml`](adapters/template.yaml).

## Team dashboard

`muretto login` and `muretto sync on` send your sessions and resources to a shared dashboard, with a live feed, a risk panel and a review queue. Both are optional. What sync sends is listed field by field in [`docs/SYNC_PAYLOAD.md`](docs/SYNC_PAYLOAD.md).

## Contributing

`make test` runs everything with no network and no accounts. See [`CONTRIBUTING.md`](CONTRIBUTING.md), and [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) for how the parts fit together.

## License

The CLI and adapters are licensed under [Apache-2.0](LICENSE).
