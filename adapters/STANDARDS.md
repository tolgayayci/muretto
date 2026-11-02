# Standards for official adapters

An official adapter ships in `adapters/`, runs for every Muretto user, and is maintained by us. The format is in [WRITING.md](WRITING.md); this page says what an official adapter must contain and how it is reviewed. CI enforces the parts marked **(CI)**.

## Files

| File | Contents |
|---|---|
| `adapters/<slug>/adapter.yaml` | The adapter. Header comment: `# <Name> adapter. Every endpoint, field and output string here comes from adapters/<slug>/research.md (checked YYYY-MM-DD). Format: adapters/WRITING.md.` |
| `adapters/<slug>/research.md` | Research with a source URL and date for every fact. Sections: **Sources**, **Spec** (URL, version, how to watch it), **Token** (narrowest token, step by step, and what it can still do), **Resources**, **Exposure** (each rule, its endpoint and field, its caveat), **Creation paths** (CLI, MCP, API, Stripe Projects: exact output strings), **Never call** (endpoints that return secrets or write), **Open questions**, and **Live verification** (filled in once checked against a test account). |
| `adapters/<slug>/coverage.md` | Every resource kind the provider has: `full`, `partial` or `planned`, with the reason. |
| `adapters/<slug>/testdata/scan/<scenario>/` | `routes.yaml`, `expect.yaml` and JSON bodies. At least `basic` and one failure scenario **(CI)**. |
| `adapters/<slug>/testdata/extract/cases.yaml` | Extract cases **(CI)**. |

The slug is the brand in lower case (`github`, `cloudflare`, `neon`, `stripe`, `render`, `planetscale`).

## Naming

- **Kinds:** singular snake_case, with no provider prefix (`repo`, `r2_bucket`, `webhook_endpoint`). A kind under a parent is named for what it is, not for its URL.
- **Endpoints:** named for the kind they list, or for what they fetch (`repo_pages`).
- **Rules:** `<state>_<kind>` (`public_repo`, `insecure_webhook`, `open_database`).
- **Signatures:** `<slug>.<channel>.<action>`, where channel is `cli`, `mcp`, `api` (curl), `stripe_projects` or `file`.
- **Attributes:** the provider's field name in snake_case. Use the same names across kinds of one adapter (`project_id`, never `projectId` in one place and `project` in another).

## Read-only and secrets

- **GET only.** A POST needs a body pinned in Go (`internal/broker/pinned.go`) and a security review.
- **Never call:** every endpoint that returns a secret value or writes is listed in the research doc. Endpoints that return secrets go into the broker's Go deny list (`deniedPaths`, a Go change reviewed with the adapter), even though the adapter never lists them.
- **`strip`** lists every response field that can hold a secret: passwords, tokens, keys, connection strings, plaintext environment-variable values, signing secrets, URLs with embedded credentials or unauthenticated trigger URLs. The broker's built-in strip list doesn't count as yours.
- **Attributes:** never secrets, and never personal data (emails, names, IPs) unless a rule needs them.
- **Token formats** the provider issues go into `internal/redact` (a Go change), so tokens an agent prints never reach the spool.

## Tokens

`token_help` names the narrowest credential that works, step by step, with the exact permissions. If the provider has no read-only credential, say plainly what the token can do. It must not ask for permissions the adapter doesn't use.

## Accounts

The account is what the token belongs to: the user's own org, team, workspace or account. Never a global list such as "every organization on GitHub". Where the provider has no endpoint for it, use `ask` and say what to type.

## Scanning

- **Every list that paginates declares `pagination`**, in whichever of the four styles the API uses. A list that can't be paged completely is `partial` in `coverage`, with the reason. Truncation is never silent.
- **Pin API versions** with `headers` where the provider supports it (`X-GitHub-Api-Version`, `Stripe-Version`), and record the version in the research doc.
- **Keep fan-out bounded:** child kinds multiply calls. Note the calls per scan in the research doc.
- **`created_at`** comes from the provider's own creation time. Leave it out if it isn't the creation time (Supabase's pre-provisioned projects).

## Exposure rules

- **Three-valued:** missing data is `unknown`, never `not_exposed`. Every rule's inputs are checked by its source (`checks_rules`).
- **Severity:**
  - `critical`: data readable by anyone
  - `high`: publicly reachable or abusable
  - `medium`: weakened protection
  - `low`: hygiene or cost
  - `info`: inventory only
- **Wording:** `caveat` says what the API can't see. `fix` is a command, SQL or steps a human runs; Muretto never runs it. No claims beyond the field (say "the API reports …", not "attackers can …").
- **Fixtures:** every rule has a fixture case for `exposed`, `not_exposed` and `unknown` where the API allows each.

## Signatures (attribution)

- **Coverage:** every documented creation path, from CLI text and JSON output, MCP results, curl against the API, and Stripe Projects. A signature's output patterns come from real or documented output, quoted in the research doc.
- **Role:**
  - `created` only when the call's success proves the resource was created;
  - reads, lists and create-or-keep calls are `referenced`;
  - failed calls yield nothing.
- **Container:** set `container` only when the creating call named it.
- **Tests:** every signature has an extract case **(CI)**. Every `created` signature has a case whose `matches:` lists the fixture resource it creates **(CI)**, which proves attribution finds it. Also include at least one case that proves nothing **(CI)**: a failure, or a read-only call.

## Fixtures

- **Values:** sanitized, with obvious fakes only (`example-org`, `prj_EXAMPLE…`, `@example.com`).
- **Recorded first:** use responses recorded from a test account where one exists (`TestRecordLive`). Otherwise build them from the provider's documented examples, and say so in the research doc.
- **Secret markers:** every stripped field holds `MURETTO_FAKE_SECRET`, and the scenario lists it under `forbidden` **(CI)**.
- **Failure scenario:** at least one, where an endpoint answers 403 or 500, so failed checks show as errors **(CI)**.

## Review gate

Before merging an official adapter:

1. `muretto adapter test adapters/<slug>/adapter.yaml --strict` passes, as does `go test ./internal/adapters ./internal/adaptercheck ./internal/broker ./internal/redact` **(CI)**.
2. The research doc cites a source for every endpoint, field and output string.
3. Go changes (deny list, redaction) are reviewed as security changes and noted in `docs/SECURITY.md`.
4. **Live verification**, read-only against a test account: every kind lists, every rule decides, and the research doc's "Live verification" section is filled in. Until then the adapter is marked `# Not yet verified live` in its header and in `docs/STATUS.md`.
