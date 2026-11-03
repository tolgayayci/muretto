# Muretto

**See what your AI coding agents created outside the repo.**

Coding agents like Claude Code create real infrastructure: databases, tables, storage buckets, functions, deployments, webhooks. Your repo shows the code they wrote, but not what they made in your provider accounts.

Muretto is an open-source CLI that lists those resources, traces each one to the agent session that created it, and flags what is open to anyone on the internet. It is **read-only**: it never creates, changes or deletes anything in your accounts.

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
  → muretto why customers
```

## Install

```sh
npm install -g muretto
```

Or build from source with Go 1.27.3 or newer:

```sh
go install github.com/tolgayayci/muretto/cmd/muretto@latest
```

Homebrew (`brew install tolgayayci/tap/muretto`) and the [`install.sh`](install.sh) script come with the GitHub release. macOS and Linux, arm64 and amd64.

`npx muretto` works for a quick look, but install it before running `muretto init`: the hooks need a binary that stays put. More in [`docs/INSTALL.md`](docs/INSTALL.md).

## Quick start

```sh
muretto init              # add Claude Code hooks and connect your providers
```

Use Claude Code as usual, then:

```sh
muretto                   # what your agents created and what is open to anyone
muretto session <id>      # everything one session created
muretto exposed           # what anyone on the internet can reach, worst first
muretto why <resource>    # the evidence behind a resource's label
muretto doctor            # check hooks, tokens and the last scans
muretto uninstall         # remove exactly what init added
```

`muretto help` lists every command. Read commands accept `--json`.

## Providers

**Supabase** and **Vercel**, plus **GitHub**, **Cloudflare**, **Neon**, **Stripe**, **Render** and **PlanetScale**. The last six are built against each provider's documented API and tested offline, but not yet against a live account. `muretto providers --coverage` shows what each one lists.

Each provider is one folder in [`adapters/`](adapters/). Agents: Claude Code today.

## How it works

1. A small Claude Code hook records each tool call, redacted, to a local spool. It has no network access and exits in milliseconds.
2. Muretto lists what really exists in your accounts through each provider's read-only API.
3. A resource is linked to a session only when that session's own tool call created it. Everything else is labelled **creator unknown**. Muretto never guesses.
4. Each adapter declares what counts as open to anyone, such as a table readable with the public key or a public bucket. A check that didn't run shows as `unknown`, never "not exposed".

Details: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Privacy

- **Read-only, enforced in code.** Every provider request goes through one broker that allows only listed read endpoints.
- **Local by default.** Tokens live in your OS keychain. Secrets are redacted. No telemetry.
- **Optional dashboard.** `muretto login` and `muretto sync on` send a listed set of fields ([`docs/SYNC_PAYLOAD.md`](docs/SYNC_PAYLOAD.md)) to app.muretto.ai or your own server. Both are off by default.

More in [`docs/SECURITY.md`](docs/SECURITY.md). To report a vulnerability, see [`SECURITY.md`](SECURITY.md).

## Contributing

`make test` runs everything with no network and no accounts. To add a provider, start with [`adapters/README.md`](adapters/README.md): a community adapter is one folder of YAML and fixtures, with no Go. See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the repo map and dev setup.

## License

[Apache-2.0](LICENSE)
