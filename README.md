# Muretto

**See what your AI coding agents created outside the repo.**

Coding agents like Claude Code and Codex now create real infrastructure: databases, tables, storage buckets, functions, deployments, webhooks. Your repo shows the code they wrote. It doesn't show what they made in your provider accounts.

Muretto is an open-source CLI that lists those resources, traces each one to the agent session and the tool call that created it, and flags what is open to anyone on the internet. It is **read-only**: it never creates, changes or deletes anything in your accounts.

```
$ muretto session 8f2a
Session 8f2a · alice · Claude Code · Oct 19 14:02–14:31
Task: "add customer import with file uploads"
Repo: acme/web (feat/import)

  SUPABASE  project acme-dev
  ✓ table      customers        exact   ⚠ RLS off: publicly readable
  ✓ bucket     customer-files   exact   ⚠ public
  ✓ function   process-import   exact
  ✓ branch     import-test      exact   7 days old · review due

  VERCEL  project acme-web
  ✓ deployment preview-a81f     exact   ⚠ public preview

  3 exposed · 1 needs review · checked 3 min ago
  → muretto why customers   → muretto fix customers
```

The names are fictional; this is the CLI's own test output (`internal/render/testdata/session.golden`).

## How it works

1. **Hooks record what the agent did.** A small Claude Code hook writes one redacted file per tool call into a local spool (`~/.muretto/spool`) and exits in milliseconds, with no network access. Muretto also reads Claude Code transcripts to fill gaps.
2. **Read-only scans list what exists.** Muretto lists what is really in your provider accounts through their APIs. Every request goes through one broker that allows only an explicit list of read endpoints and refuses everything else before it reaches the network.
3. **The join.** A resource is linked to a session only when that session's own tool call created it: the provider id (or deployment URL) appears in the call's output, or a unique name appears in a call that targeted that project. Everything else is labelled **creator unknown**. Muretto never guesses, and `muretto why` shows the evidence behind every label.
4. **Exposure checks.** Each provider adapter declares rules for what is open to anyone: a table readable with the public key, a public bucket, a public preview deployment, a database open to `0.0.0.0/0`. Each result is `exposed`, `not exposed` or `unknown`; Muretto never reports "not exposed" for a check that did not run.

[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) has the details.

## Supported providers

| Provider | What Muretto lists |
|---|---|
| **Supabase** | projects, branches, edge functions, storage buckets; tables (agent-created and advisor-flagged, or all of them with an opt-in) |
| **Vercel** | projects, deployments (preview URLs), domains, env var names |
| **GitHub** ¹ | repos, Pages sites, repo and org webhooks, deploy keys, environments, Actions variable names, org app installations |
| **Cloudflare** ¹ | R2 buckets and custom domains, Workers, Pages projects and deployments, D1, KV, zones, DNS records, tunnels and their routes |
| **Neon** ¹ | projects, branches, computes, databases (with Data API state), roles |
| **Stripe** ¹ | webhook endpoints, products, prices, payment links, billing portal configurations; live and test as separate accounts |
| **Render** ¹ | services, Postgres, Key Value, custom domains, Blueprints, env groups (opt-in) |
| **PlanetScale** ¹ | databases, branches, passwords, Postgres roles, deploy requests |

¹ Built against each provider's documented API and tested offline. Not yet checked against a live account.

Each provider is one YAML file in [`adapters/`](adapters/), with its sources and a coverage list of what is planned. `muretto providers --coverage` prints the same matrix. Agents: Claude Code today.

## Install

**There is no release yet.** Until v0.1.0 is tagged, install from source (Go 1.27.3 or newer):

```sh
go install github.com/tolgayayci/muretto/cmd/muretto@latest
```

Or build from a clone:

```sh
git clone https://github.com/tolgayayci/muretto && cd muretto
make build          # writes bin/muretto
```

From v0.1.0, these will also work:

```sh
npm install -g muretto
brew install tolgayayci/tap/muretto
curl -fsSL https://raw.githubusercontent.com/tolgayayci/muretto/main/install.sh | sh   # verifies checksums
```

macOS and Linux, arm64 and amd64. Details: [`docs/INSTALL.md`](docs/INSTALL.md).

## Use

```sh
muretto init        # add Claude Code hooks (backed up, removable) and connect providers
```

Then use Claude Code as usual. The main commands (`muretto help` lists them all, including `adapter`, `login` and `sync`):

```
muretto                      summary: agent-created, exposed, needs review, recent sessions
muretto sessions             recent agent sessions
muretto session <id>         everything one session created
muretto resources [--provider p] [--exposed] [--unknown]
muretto show <resource>      one resource: timeline, creator, exposure, cost, review
muretto why <resource>       the evidence behind its attribution label
muretto exposed              exposure findings, worst first
muretto fix <resource>       the provider's documented steps to close an exposure
muretto review [<resource> keep|not-needed]
muretto providers [--coverage]
muretto doctor               check hooks, tokens, spool and last checks
muretto uninstall [--purge]  remove exactly what muretto init added
```

Read commands accept `--json`, `--refresh` (ignore the 60 s scan cache) and `--no-color`. Session and resource arguments accept unique prefixes and names.

`muretto fix` only prints text. It shows the provider's documented command or SQL for closing an exposure (and, if you no longer need the resource, how to remove it), for you to review. Muretto never runs it.

## Optional dashboard

`muretto login` and `muretto sync on` send your sessions and resources to a personal web dashboard ([`apps/dashboard`](apps/dashboard/)). Both are off by default, and `sync on` prints every field it will send before it asks. The dashboard is personal-only today: you see your own data. Team features are marked "soon".

The hosted dashboard is app.muretto.ai. You can run your own server and dashboard instead; see [`server/README.md`](server/README.md) and [`apps/dashboard/README.md`](apps/dashboard/README.md).

## Privacy and security

- **Read-only, enforced in code.** All provider requests go through `internal/broker`, which allows only listed read endpoints. A test fails if any other code makes HTTP requests.
- **What leaves your machine.** Without sync, only read-only API requests to the providers you connect, with your own tokens. With sync on, the fields listed in [`docs/SYNC_PAYLOAD.md`](docs/SYNC_PAYLOAD.md), and never tokens, raw transcripts, full prompts, local paths or secret values.
- **Secrets are redacted** when recorded and again on output and sync. Tokens live in your OS keychain. Env var values are never stored.
- **No telemetry.**

The full model, including what your tokens can do, is in [`docs/SECURITY.md`](docs/SECURITY.md). To report a vulnerability, see [`SECURITY.md`](SECURITY.md).

## Repository map

| Path | What |
|---|---|
| [`cmd/muretto`](cmd/muretto/) | The CLI |
| [`internal/`](internal/) | Hook, redaction, broker, adapter engine, attribution, store, rendering, sync client |
| [`adapters/`](adapters/) | One folder per provider: `adapter.yaml`, research, coverage, fixtures |
| [`schema/`](schema/) | JSON Schema for every shared type; Go and TypeScript types are generated from it |
| [`server/`](server/) | The sync API (`murettod`) |
| [`apps/`](apps/) | The dashboard, the muretto.ai site and the docs site |
| [`plugin/`](plugin/) | The same hooks as a Claude Code plugin |
| [`packaging/npm`](packaging/npm/), [`install.sh`](install.sh) | Distribution |
| [`examples/live-demo`](examples/live-demo/) | Optional live demo against your own test accounts |
| [`docs/`](docs/) | Architecture, security model, decisions and research |

## Contributing

`make test` runs everything with no network and no accounts. See [`CONTRIBUTING.md`](CONTRIBUTING.md). To add a provider, start with [`adapters/README.md`](adapters/README.md): a community adapter is one folder (YAML plus fixtures) with no Go; an official one also adds a few lines of Go (see [`adapters/WRITING.md`](adapters/WRITING.md)). Have an OpenAPI file? `muretto adapter new <openapi-file>` drafts one.

## License

[Apache-2.0](LICENSE), for the whole repository.
