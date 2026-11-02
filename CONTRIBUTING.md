# Contributing to Muretto

Thanks for helping. Muretto is a read-only tool: it lists what coding agents
created in provider accounts and flags what is open to anyone. It never
creates, changes or deletes anything in a provider account, and every
contribution has to keep that promise.

The most useful contribution is a new provider adapter. It is YAML and test
fixtures, and you don't need a provider account. Start at
[`adapters/README.md`](adapters/README.md).

## Repo map

| Path | What it is |
|---|---|
| [`cmd/muretto`](cmd/muretto) | The CLI (`main` package): `init`, `sessions`, `resources`, `exposed`, `adapter`, `doctor`, … |
| [`internal/`](internal) | The CLI's packages. `broker` (the only code that talks to providers, with the read-only allowlist), `adapters` (the YAML engine), `adaptercheck` (the adapter test gate), `adaptergen` and `openapi` (drafting adapters from OpenAPI), `redact`, `hook`, `observe`, `attrib`, `store`, `syncclient`, … |
| [`adapters/`](adapters) | One folder per provider: `adapter.yaml`, research, coverage and fixtures. |
| [`schema/`](schema) | JSON Schemas for every record. Go types (`internal/contract`) and TypeScript types (`apps/dashboard/src/contract`) are generated from them. |
| [`plugin/`](plugin) | The Claude Code plugin: the hooks that record agent tool calls. |
| [`server/`](server) | `murettod`, the optional sync API behind `muretto login`, `muretto sync` and the dashboard. |
| [`apps/dashboard`](apps/dashboard) | The web dashboard (Next.js). |
| [`apps/site`](apps/site) | The muretto.ai landing page (a Cloudflare Worker with static assets). |
| [`apps/docs`](apps/docs) | The documentation site (Mintlify). |
| [`packaging/npm`](packaging/npm) | The npm packages that install the prebuilt binary. |
| [`examples/live-demo`](examples/live-demo) | An optional end-to-end run against your own **test** accounts. It creates and deletes real resources. |
| [`docs/`](docs) | Architecture, security model, install notes, the sync payload and design decisions. |
| `tools/` | Pinned Go tools (gitleaks, govulncheck, go-jsonschema), run with `go tool -modfile=tools/go.mod`. |

[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) explains how the parts fit
together.

## Dev setup

### Go: CLI, adapters and server

You need Go (the version in [`go.mod`](go.mod)) and `make`.

```sh
git clone https://github.com/tolgayayci/muretto
cd muretto
make setup          # git hooks: secret scan and gofmt before every commit
make test           # the whole Go suite: no network, no accounts
make help           # every documented target
```

Run the CLI from source:

```sh
go run ./cmd/muretto --help
go run ./cmd/muretto adapter list
make build          # or build bin/muretto once and run that
```

Other targets: `make adapter-test` (the adapter gate, see below), `make lint`
(needs golangci-lint v2.14.0; CI runs it), `make vuln`, `make secrets`, and
`make generate` after changing anything in `schema/`. `make ci` runs what CI
runs for the Go code.

To run the sync API locally with no database and no Supabase project, see
[`server/README.md`](server/README.md):

```sh
MURETTO_DEV_LOGIN=1 go run ./server/cmd/murettod
```

### Dashboard

Node.js 20.9 or newer (CI uses 24).

```sh
cd apps/dashboard
npm ci
npm run dev         # http://localhost:3100
npm run lint && npm run typecheck && npm test && npm run build   # what CI runs
```

Sign-in without a Supabase project, and the sample data mode, are described in
[`apps/dashboard/README.md`](apps/dashboard/README.md).

### Site and docs

- **Site** (`apps/site`): `public/index.html` is the whole page. Preview it with
  `npx wrangler dev` in `apps/site` (http://localhost:8787). See
  [`apps/site/README.md`](apps/site/README.md).
- **Docs** (`apps/docs`): Mintlify pages in MDX. Preview with `npx mint dev` in
  `apps/docs` (http://localhost:3000).

## Adding a provider

Read [`adapters/README.md`](adapters/README.md). In short:

```sh
mkdir -p adapters/example
go run ./cmd/muretto adapter new openapi.json --provider example \
  -o adapters/example/adapter.yaml --fixtures adapters/example/testdata
# edit adapter.yaml, write research.md, coverage.md and the fixtures, then:
make adapter-test P=example
```

- The adapter itself is YAML. A pull request that adds one folder under
  `adapters/` and passes `go test ./adapters/` can be merged as a community
  adapter.
- Fixtures can be built by hand from the provider's documented response shapes.
  You don't need a test account. Use the fake values in
  [`adapters/FIXTURES.md`](adapters/FIXTURES.md).
- An **official** adapter also meets
  [`adapters/STANDARDS.md`](adapters/STANDARDS.md), is verified against a live
  test account, and needs small changes outside its folder: the broker's deny
  list (`internal/broker/pinned.go`), token formats for redaction
  (`internal/redact/redact.go`), the dashboard's provider list and the README
  table. Maintainers can make those. See
  [`adapters/WRITING.md`](adapters/WRITING.md#11-official-adapters-changes-outside-the-folder).
- To ask for a provider without writing it, open a
  [provider request](https://github.com/tolgayayci/muretto/issues/new?template=provider_request.md).

## Ground rules

- **Read-only.** Provider network calls go only through `internal/broker`, which
  allows an explicit list of read endpoints. Don't add a request path anywhere
  else.
- **No secrets, ever.** Not in code, fixtures, docs, screenshots, issues or
  commit messages. The pre-commit hook and CI run gitleaks, and a test scans
  every adapter fixture. If you leak a token by accident, revoke it first, then
  tell us.
- **No real account data.** Use obvious fakes: `example-org`, `example-team`,
  `customers`, `waitlist-preview`, `@example.com`. Never paste real project
  refs, URLs or keys.
- **Honest output.** A label is `exact` only with stored evidence. Missing data
  is `unknown`, never "not exposed", and unknown cost is never shown as `$0`.
- **Few dependencies.** Justify each new dependency in your pull request.
- **Security changes** (the broker, redaction, the hooks, sync) update
  [`docs/SECURITY.md`](docs/SECURITY.md) in the same pull request.

## Pull requests

- One topic per pull request. Open an issue first for anything large.
- Use the template. Adapter pull requests have their own:
  add `?template=adapter.md` to the compare URL.
- `make test` passes. For the dashboard, its lint, typecheck, tests and build
  pass.
- CI runs on every pull request from a fork with a read-only token and no
  secrets. Labels (`provider:<slug>`, `area:<part>`) are added automatically.
- New behaviour has a test. Tests never call a real provider.
- Non-obvious design decisions get a short record in
  [`docs/DECISIONS.md`](docs/DECISIONS.md) and
  [`docs/decisions/`](docs/decisions).

### Commit style

One line, at most 72 characters, in the imperative ("Add the Fly.io adapter",
"Fix pagination for Render env groups"). An optional area prefix is fine
("Docs: …", "Server: …"). No body is needed; explain the why in the pull
request.

## Live tests

Tests against real **test** accounts are opt-in and off by default
(`MURETTO_LIVE=1`). They never run in CI. See
[`adapters/FIXTURES.md`](adapters/FIXTURES.md#recording-real-responses).

## Conduct and contact

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md). Report security
issues privately as described in [SECURITY.md](SECURITY.md), never in a public
issue.

Conduct reports: [tolga@yk-labs.com](mailto:tolga@yk-labs.com)
