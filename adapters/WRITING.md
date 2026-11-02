# Writing an adapter

This is the format reference. For the short version (folder layout, the five
steps, what CI checks), read [README.md](README.md) first.

An adapter teaches Muretto one provider: how to list what exists (read-only),
how to recognise an agent creating something, and which exposure rules apply.
It is **one folder, `adapters/<provider>/`**, holding `adapter.yaml`, a
`research.md` with sources, a `coverage.md`, and fixtures in `testdata/`.
The adapter itself is YAML only. For what an official adapter also changes
outside its folder, see [section 11](#11-official-adapters-changes-outside-the-folder).

Start from [`_template/adapter.yaml`](_template/adapter.yaml): every field
is there with a comment. The shipped adapters are full examples:
[`supabase/adapter.yaml`](supabase/adapter.yaml) and
[`vercel/adapter.yaml`](vercel/adapter.yaml).

The commands below are written as `muretto …`. Working from a clone, run
`go run ./cmd/muretto …` from the repo root instead, or `make build` once and
use `bin/muretto`.

**Have an OpenAPI file?** Generate a draft instead of starting from scratch
(section 0.5). **Shipping it with Muretto?** Official adapters also follow
[STANDARDS.md](STANDARDS.md).

## 0.5 From an OpenAPI description

```sh
curl -o openapi.json https://api.example.com/openapi.json   # Muretto never fetches it
muretto adapter new openapi.json --list           # candidate kinds, nothing written
muretto adapter new openapi.json                  # writes <provider>.yaml and a report
muretto adapter check example.yaml                # validate; shows every request it may send
muretto adapter test example.yaml --openapi openapi.json   # offline dry run against the spec
muretto adapter install example.yaml              # ~/.muretto/adapters/, used from now on
muretto init                                      # connect a token
```

To contribute it, write it straight into its folder with starter fixtures
(`-o` does not create the folder):

```sh
mkdir -p adapters/example
go run ./cmd/muretto adapter new openapi.json --provider example \
  -o adapters/example/adapter.yaml --fixtures adapters/example/testdata
```

It reads Swagger 2.0 and OpenAPI 3.0, 3.1 and 3.2, in JSON or YAML. A spec split
across files works when its `$ref`s are relative and stay inside the spec's
directory. Everything it writes comes from the spec, and each endpoint carries a
`# GET /path (operationId)` comment saying where it came from. It generates:

- **`api` and `token_help`**: the https server (the server's path, e.g. `/v1`,
  moves into each endpoint path) and the security scheme most GET operations
  use. Bearer, OAuth and OpenID Connect tokens, API-key headers and basic auth
  are supported. A token in a query string or cookie is refused.
- **Accounts**, in this order: an org or team list that other operations refer
  to (`/orgs/{org}/…` or a `teamId` query parameter); else a current-user object
  (`/me`, `/user`, `/v2/account`); else `ask`, when list calls need an account
  the API cannot list (Fly's required `org_slug`, so `muretto init` asks for
  it, or reads `MURETTO_<PROVIDER>_ACCOUNT`); else one implicit account per
  token.
- **Kinds** from GET list endpoints (a root array, or an array-of-objects field
  like `data` or `projects`). Path parameters make a kind a child of the list
  that fills them (`/projects/{id}/databases`). So does a required query
  parameter named after a kind (`?projectId=`). The newest of `/v9/x` and
  `/v10/x` wins.
- **Fields**: `id` (the field child paths address, else `id`, `uid`, `uuid`,
  `ref`, then `slug`, `name`, …), name, status, url, `created_at`, and up to 12
  attributes. Booleans, enums and exposure-looking fields come first; timestamps,
  links, free text and personal data are left out.
- **`strip`** for every secret-looking response field: `writeOnly`,
  `format: password`, `x-ms-secret`, and names like `password`, `client_secret`,
  `api_key`, `connection_string` or `*_token`.
- **Pagination** when the body carries a cursor that a query parameter takes
  back. That covers `next_cursor` ↔ `cursor`, Vercel's `pagination.next` ↔
  `until`, Hetzner's `meta.pagination.next_page` ↔ `page`, and the
  Speakeasy/Fern extensions. Link headers, next-page URLs, `starting_after`,
  offsets and page numbers without a body cursor are written as `partial:
  first page only` in `coverage`.
- **`remove`** commands from `DELETE <list>/{id}`, and **curl signatures** from
  `POST <list>`. A child's container is read from the URL the agent posted to.
- **Suggested rules** (`public_<kind>`), marked REVIEW, for boolean `public` or
  `is_public` fields, `private` fields, and enum `visibility` fields with a
  public value. The fix names the PATCH endpoint if the spec has one.

A kind is selected only if the spec can create or delete its items, or it has
such a public field. Read-only lists (advisors, usage, logs) are listed in
`coverage` as `planned`, and `--kinds` turns them on. Endpoints that may return
secrets (`/secrets`, `/api-keys`, `/tokens`, …), and endpoints the broker denies
in Go, are never generated.

An OpenAPI file cannot say what a provider's CLI prints, what its MCP tools are
called, or what really makes something exposed. So the draft has no CLI
signatures (add them with section 7). It has MCP signatures only with
`--mcp-server <name>`, which assumes tools are named after operationIds.
Review every line marked REVIEW. Options: `--provider`, `--name`, `--base-url`,
`--kinds`, `--max-depth`, `--accounts <path>|none`, `--mcp-server`,
`--no-rules`, `--include-deprecated`, `-o`, `--install`, `--json`.

Installed adapters (`~/.muretto/adapters/*/adapter.yaml`) load after the built-in
ones. They cannot reuse a built-in provider's name and may only use GET. A
broken file is reported and skipped. To ship one with Muretto, put it in
`adapters/<provider>/` with fixtures (section 9) instead.

## 0. Rules before you start

1. **Read-only.** Muretto never creates, changes or deletes anything. Every
   request goes through `internal/broker`, which refuses, before any network
   I/O, anything that is not an endpoint you listed: other hosts, other paths,
   unlisted query parameters, every method except GET, redirects to other
   hosts, and anything mentioning `decrypt`. A POST is possible only if its
   exact body is declared in Go (`internal/broker/pinned.go`), reviewed like
   any security change. YAML can name such a body but never define one.
2. **Never invent.** Every endpoint, field, CLI output string and MCP tool name
   must come from the provider's docs, OpenAPI spec or source. Write what you
   found, with links and the date, to `adapters/<provider>/research.md` first.
3. **Store only what you list.** Attributes are an explicit allowlist. Nothing
   else from a response is kept. Secret-bearing fields must also be listed
   under `strip` so the broker drops them before anything else sees the body.
4. **Honest states.** A failed call is never "nothing found". The engine turns
   failures into error checks, and rules that depend on missing data say
   `unknown`.
5. **No real data in fixtures.** Obvious fakes only (`example-org`,
   `prj_EXAMPLE…`, `@example.com`). A test fails on anything secret-shaped.

## 1. Header and API

```yaml
format: 1
provider: example          # slug; kinds become example.<kind>
display_name: Example Cloud
api:
  base_url: https://api.example.com
  auth: { header: Authorization, scheme: Bearer }
mcp_servers: [example]     # MCP server names this provider answers to
token_help: |              # shown by `muretto init`
  The least-privilege token, step by step.
```

`mcp_servers` matching is case-insensitive and accepts Claude Code's naming
variants: `mcp__example__tool`, `mcp__claude_ai_Example__tool` (claude.ai
connectors) and `mcp__plugin_<plugin>_example__tool`.

`token_help` must name the narrowest token that works and say plainly what it
can do if the provider has no read-only token.

## 2. Endpoints: the allowlist

```yaml
endpoints:
  projects:
    path: /v1/projects
    query: { org: "{account}", per_page: "100" }
    pagination: { next: meta.next_cursor, param: cursor }
    strip: ["projects[].connection_string"]
  databases:
    path: /v1/projects/{project}/databases
    params: { project: "{parent.id}" }
    on_status: { 404: empty }
```

- `path`: `{var}` matches exactly one URL segment. Each var needs a `params`
  template. A trailing slash is kept and must be sent exactly (`/api/0/orgs/`).
- `query`: the only query names the broker will allow, with value templates.
  A parameter whose template has no value is not sent.
- `pagination`: how to get the next page. `param` is the query parameter
  that carries it (it joins the allowlist). Choose one `style`:
  - `cursor` (default): `next` is the JSON path of the cursor in each
    response; it is sent back as `param` until missing or null (Vercel's
    `pagination.next` → `until`; Hetzner's `meta.pagination.next_page` →
    `page`). With `items`, an empty page also ends the list (Neon sends a
    cursor on every page).
  - `link`: read `param` (e.g. `page`) from the `Link` header's `rel="next"`
    URL and send it on the original path (GitHub, Netlify). The link must be
    https on the same host; nothing else is taken from it.
  - `page`: send `param` = 2, 3, … while `more` (a boolean path) is true, or
    while `total` (JSON path of the page count, e.g.
    `result_info.total_pages`) says there are more. When the body has neither,
    `items` keeps paging until a page is empty. `start` sets the first page
    (default 1).
  - `last_item`: send `param` = `field` of the last item in `items` (default
    `$`) while `more` is true, or until a page is empty (Stripe
    `starting_after` + `has_more`; Render's per-item `cursor`).

  `max_pages` (default 50) turns runaway paging into an error check, and a
  repeated cursor is an error too: results are never silently truncated.
- `headers`: constant request headers, for pinning an API version
  (`X-GitHub-Api-Version: "2022-11-28"`, `Stripe-Version`). Auth, cookies,
  framing and method-override headers are refused.
- `strip`: JSON paths the broker deletes from the response. `a[].b` walks
  arrays; a leading `[]` walks a root array. The broker additionally always
  strips `value`, `vsmValue`, `legacyValue`, `protectionBypass`, `deployHooks`,
  `jwt_secret`, `db_pass`, `service_role_key`, `smtp_pass` and `claimUrl` at
  any depth. You cannot use those field names as attributes.
- `on_status: { 422: empty }`: treat that status as a successful empty list
  (Supabase answers 422 for branches when branching is off). On an
  `enrich` endpoint it means "the thing does not exist": the enrichment's
  attributes become null, so rules can decide instead of saying unknown
  (Neon answers 404 when a branch has no Data API).
- `method: POST` requires `pinned_body: <name>` naming a body in
  `internal/broker/pinned.go`. Adding one is a Go change with a security
  review and a note in `docs/SECURITY.md`.

The broker also denies some endpoints in Go even if you list them (for example
Supabase `/v1/branches/{id}`, which returns database passwords, and Vercel
single env var reads, which return decrypted values).

## 3. Accounts

```yaml
accounts:
  endpoint: orgs
  items: orgs         # JSON path to the array; "$" is the body itself
  id: "{id}"
  name: "{name}"
  slug: "{slug}"      # optional; the CLI passes it back as Credentials.AccountSlug
account_slug: "{account}"   # optional fallback when no slug is passed
```

Without an endpoint, either name one implicit account with constants
(`accounts: { id: default, name: Example }`, for APIs where a token is
one account), or set `accounts: { ask: "Your Example org slug" }` to make
`muretto init` ask for the id (or read `MURETTO_<PROVIDER>_ACCOUNT`).

Every resource gets `attributes.account_slug`, which name-based identifiers
(e.g. a team slug printed by a CLI) are matched against.

`token_modes` names a token by its shape, for providers whose live and test
keys reach the same account (Stripe): `token_modes: { live: '^(sk|rk)_live_',
test: '^(sk|rk)_test_' }` makes `{token_mode}` usable in the account `id`,
`name` and `slug` templates, so the two are separate accounts. The token
itself never reaches a template.

## 4. Templates

Most values are templates: text with `{path|filter}` placeholders.

- Paths are dotted JSON paths into the current context. In a list source the
  context is the item; `parent.` is the `for_each` parent (its raw fields and
  its attributes); `account`, `account_name` and `account_slug` describe the
  account. These four names always mean the scan context: an item field
  called `parent` or `account` cannot be read in templates.
- Filters: `or:<path>` (fallback path), `default:<text>`, `lower`, `upper`,
  `trim`, `join` (sorts an array and joins it with `,`), `unquote`,
  `pg_ident` (a Postgres identifier as written: `"Quoted"` keeps its case,
  bare names fold to lower case), `sql_ident` (escape for `"…"`),
  `sql_literal` (escape for `'…'`).
- A placeholder with no value (missing, null or empty) makes the whole
  template empty. For an `id` that means the item is skipped and the check
  reports it. For human text (titles, fixes) the hole is shown as `<path>`.

## 5. Kinds

```yaml
kinds:
  - kind: database
    for_each: project            # one call per project found earlier
    sources:
      - endpoint: databases
        items: databases         # or alternatives: [items, $]
        where: { field: deleted, is_null: true }
        checks_rules: [public_database]
        id: "{parent.id}/{name}" # provider_id; resource id = example.database:<id>
        name: "{name}"
        status: "{state|lower}"
        url: "https://console.example.com/db/{name}"
        created_at: { value: "{created_at}", format: auto }
        attributes:
          - region                                   # same name
          - { name: public, field: public_access }   # rename / nested path
          - { name: project_id, template: "{parent.id}" }
          - { name: tier, field: plan.tier, default: free }
    enrich:                       # optional extra GET per resource
      - { endpoint: db_settings, requires_option: extra, attributes: [ssl_enforced] }
    match: { value: "{name}", container: "{project_id}" }
    remove: { kind: command, text: "example db delete {name} --project {project_id}" }
```

- `for_each` must name an earlier kind. Each parent becomes the check's
  `scope` and the resource's `parent_id`. `for_each_where` (a predicate on
  the parent) skips parents it doesn't apply to, with no calls and no checks
  (only Vitess databases have deploy requests). A source can instead set `parent` +
  `parent_kind` from an item field.
- `id` must be unique within the kind and stable (it becomes
  `<provider>.<kind>:<id>`). Use natural keys like `<project>/<name>` where the
  provider has no id.
- `created_at.format`: `rfc3339`, `epoch_ms`, `epoch_s` or `auto` (numbers
  above 1e11 are milliseconds).
- `attributes`: only scalars (strings, numbers, booleans, null). Arrays of
  scalars are joined with `,`. A field that is JSON `null` is stored as null;
  a missing field is left out. Rules rely on that difference.
  Reserved names: `id`, `provider_id`, `name`, `kind`, `account_id`,
  `account_name`, `account_slug`, `url`, `status`, `parent_id`, `parent`,
  `created_at`.
- `where` drops items that are not definitely true (see predicates below).
- `unavailable_if`: if any item matches, the check fails with "provider
  reported the check unavailable" (Supabase's `advisor_check_unavailable`
  lint), so rules relying on it stay unknown. `incomplete_if: <body path>`
  does the same when the response itself says items were left out (Neon's
  `unavailable_project_ids`).
- An attribute `field` may step through arrays with `[]`:
  `field: "ipAllowList[].cidrBlock"` is every element's value, joined with
  `,` (quote it inside `{ … }`). A failed `enrich` call leaves that
  resource's enrichment attributes missing, so its dependent findings are
  unknown; other resources keep their verdicts.
- `seen_via: advisor` marks sources that only list flagged items (no plain
  kind check is emitted for them, because "none flagged" is not "none exist").
- `requires_option` runs a source or enrichment only when the user opted in
  (declare it under `options`; it reaches the engine as
  `Credentials.Options[name] = true`).
- Two sources may produce the same resource id; attributes are merged and a
  `list` source wins over an `advisor` one.

### Checks

Each source call produces a `ProviderCheck` per scope: one for the kind (for
`list` sources) and one per rule in `checks_rules`, with status `ok` or `error`
(`auth`, `permission`, `not_found`, `rate_limit`, `provider`, `network`,
`unsupported`). Downstream, a resource is `not_exposed` for a rule only if its
latest rule check is `ok`.

## 6. Exposure rules

```yaml
rules:
  - id: public_database
    kind: database
    severity: high          # critical | high | medium | low | info
    signal: api_field       # official_advisor | api_field | catalog_query | name_pattern
    applies: { not: { field: tier, equals: internal } }
    exposed: { field: public, equals: "true" }
    # not_exposed: defaults to "exposed is false"
    title: "Public database: {name}"
    explanation: Database {name} accepts connections from the internet.
    caveat: What the API cannot see.
    fix:
      kind: steps           # sql | command | steps
      text: "Console > {name} > Networking > Public access: off"
      note: Optional extra advice.
      docs_url: https://docs.example.com/networking
```

A rule runs on every resource of its kind where `applies` is true. If
`exposed` is true the finding is `exposed`; else if `not_exposed` is true it is
`not_exposed`; otherwise `unknown`. Several rules may share an id (one per
signal, like Supabase's advisor and catalog variants of `rls_disabled`); the
worst state wins.

Rules see the resource's attributes, its core fields (`provider_id`, `name`,
`account_slug`, …) and, during a scan, `parent.<attribute>`. Fixes and
titles use the same context. **A fix is printed, never run.**

### Predicates

Predicates are three-valued: a test on a missing field is `unknown`, so a rule
cannot accidentally say `not_exposed` when an input is missing.

| Form | True when |
|---|---|
| `all: [p, …]` | every p is true (false if any is false, else unknown) |
| `any: [p, …]` | some p is true (false if every p is false, else unknown) |
| `not: p` | p is false (unknown stays unknown) |
| `field: f, present: bool` | f exists (null counts); never unknown |
| `field: f, is_null: bool` | f is (not) JSON null |
| `field: f, equals: v` | f equals v (compared as text: `"true"`, `"17"`) |
| `field: f, in: [v, …]` | f equals one of them |
| `field: f, prefix_any: [s, …]` | f starts with one of them |
| `field: f, matches: re` | f matches the RE2 regex (unanchored) |
| `field: f, contains_any: [s, …]` | f (array or comma list) contains one of them |
| `field: f, in_field: g` | f is an element of g's comma list |
| `field: f, older_than: 30d` | f (RFC 3339 or epoch) is more than 30d before the scan (`m`, `h`, `d`, `w`); `newer_than` is the opposite; not a timestamp: unknown |
| `field: f, due_within: 7d` | f is at most 7d after the scan, or already past (an expiry coming up) |

Rules also see `created_at` (RFC 3339) when the kind maps it.

A null value is false for every test except `present` and `is_null`. These
reusable predicates replace provider-specific Go: e.g. Vercel's
`public_preview` is an `all`/`any` over the parent project's `sso_protection`,
`password_protection` and `trusted_ips` attributes.

## 7. Signatures: recognising creation

Signatures read agent tool calls (`observe.RawAction`: tool name, redacted
input, redacted output) and return identifiers. They are pure: no network.

```yaml
signatures:
  - id: example.cli.db_create
    kind: database
    role: created            # created | referenced
    when:
      tool: ^Bash$           # regex on the tool name
      command: ['\bexample\s+db\s+create\b']   # all must match input.command
      # not_command, input_matches: { file_path: re }, output: re
      # status: ok (default) | any
    extract:
      - from: output         # output | command | input.<path> (regex) or result | input (templates)
        pattern: 'Created database (?P<name>[a-z0-9-]+) in project (?P<project>prj_[a-z0-9]+)'
        value: "{name}"
        container: "{project}"
```

- **MCP:** `when: { mcp_tools: [create_database], result_lacks: [error] }`
  matches `mcp__<server>__create_database` for the adapter's `mcp_servers`.
  `input_matches` works for MCP tools too, e.g. a generic `api_write` tool
  keyed on `{ method: '^POST$', path: '^/v1/products$' }`. A Bash signature
  with no `command` pattern still extracts ids, but never makes Muretto
  guess the provider of a call.
  `mcp_tools: ["*"]` matches every tool (useful for `referenced` ids).
  `from: result` parses the output as JSON: the whole output, or every JSON
  object or array that starts a line, so CLI noise around it is fine and a
  chained Bash call (`x new a; x deploy a`) that prints one JSON line per
  command yields each of them. The extract runs once per JSON value and
  emits what resolves; other extracts see the first value as `result`. Use
  `each: result.items` to iterate (each element is `item`).
- `when.result_lacks: [error]` rejects the call if **any** JSON value in the
  output has one of these top-level keys (so a chained call with one failure
  proves nothing).
- `when.result_has: [success, result]` is the positive counterpart of
  `result_lacks`: at least one JSON value must be an object with one of these
  top-level keys, and only those values feed `from: result`. Use it when the
  tool documents its success shape, so an error in another shape (plain
  text, a pending confirmation) never counts.
- `mcp.<key>` in a template is an identifier param of the MCP server the call
  went to, read by the hook from the server URL in Claude Code's config
  (`mcp.project_ref` for `https://mcp.supabase.com/mcp?project_ref=<ref>`).
  Use it as a fallback for servers pinned to one project, whose tools take no
  project id: `container: "{input.project_id|or:mcp.project_ref}"`. It is
  only set for hook-recorded calls to servers from the project, local or user
  scope, and only for allowlisted keys (decision 0905 in
  [DECISIONS.md](../docs/DECISIONS.md)).
- `preprocess` rewrites the text a regex extract runs on, in order:
  - `shell_args` splits a shell command into its arguments (quotes and
    escapes removed; a command with an unclosed quote yields nothing);
  - `sql` lexes PostgreSQL: `--` and `/* */` comments are dropped, and string
    constants (`'…'`, `E'…'`, `$$…$$`, `$tag$…$tag$`) and quoted identifiers
    are re-quoted with their content percent-encoded outside `[A-Za-z0-9_.-]`.
    So `select 'create table x'` or `-- create table x` cannot match, while
    `'customer-files'` and `"Orders"` read as before.
  Typical use: `preprocess: [shell_args, sql]` for SQL passed on a command
  line (`from: command`), `preprocess: sql` for `from: input.query`. Segments
  are joined with NUL, so a match never spans two arguments. Write SQL
  patterns for lexed text, e.g. a quoted name as `"[^"%]+"`.
- `referenced_if: <group>`: a match where that named group is non-empty
  yields role `referenced`. Use it for statements that succeed whether or not
  the thing existed (`CREATE TABLE IF NOT EXISTS`, `INSERT … ON CONFLICT`):
  they do not prove creation
  ([decision 0406](../docs/decisions/0406-proof-of-creation.md)).
- `split: { field: names, sep: "," }` turns one captured list into several ids.
- `capture: [{ from: command, pattern: '--project (?P<project>\S+)' }]` adds
  groups from a second regex, typically a flag naming the container.
- Output is ANSI-stripped before matching. Failed calls (`status: error`) are
  ignored unless `when.status: any`.
- **role:** `created` only for calls whose success proves they created the
  thing. Reads, lists, local files and create-or-keep statements are
  `referenced` (an evidence trail, never an exact label). Create-or-update
  calls (e.g. a function deploy) may stay `created`: attribution vetoes them
  when the provider's `created_at` or Muretto's own earlier scan shows the
  thing already existed.
- **value / container contract** (what attribution relies on): `value` is the
  provider id whenever the output has one. Otherwise `value` is the name and
  `container` the thing it is unique within (project, team slug). Set
  `container` **only when the creating call itself named it** (an MCP
  `project_id` input, a `--project-ref` flag, the project printed by the CLI).
  Never guess it from timing or other calls.
- **match:** how an identifier finds its resource, as templates over the
  resource: `match: { value: "{name}", container: "{project_id}" }`. Set it on
  the kind (default `{provider_id}`) and override per extract when one
  signature yields ids in another shape (e.g. a deployment URL host instead
  of its id). An identifier whose match needs a container but has none never
  matches.

Stripe Projects and similar provisioning tools are just more signatures (see
`supabase.stripe_projects.add`): recognise the command, extract ids only if the
output contains them.

## 8. Coverage, options and the recorder

```yaml
options:
  - { name: extra, description: "What it reads, what permission it needs, what that permission can see." }
coverage:
  - { kind: database, status: full, note: "GET /v1/projects/{id}/databases" }
  - { kind: bucket, status: planned, note: "No read endpoint yet" }
recorder:
  pseudonymize: [id, name, metadata.name]   # key, or parent.key
  keep: [production, preview]               # values never replaced
```

`coverage` feeds `muretto providers --coverage`: list every kind the provider
has, honestly (`full`, `partial`, `planned`).

## 9. Fixtures and tests

```
adapters/<provider>/testdata/scan/<scenario>/routes.yaml
adapters/<provider>/testdata/scan/<scenario>/expect.yaml
adapters/<provider>/testdata/scan/<scenario>/*.json
adapters/<provider>/testdata/extract/cases.yaml
```

`routes.yaml` lists what the fake API answers:

```yaml
- { method: GET, path: /v1/projects, query: { org: example-org }, file: projects.json }
- { path: /v1/projects/prj_EXAMPLE1/databases, status: 403, body: '{"message":"forbidden"}' }
```

The most specific matching `query` wins. `expect.yaml`:

```yaml
account: { id: example-org, name: Example Org, slug: example-org }
options: []                      # opt-ins for this scenario
resources: [example.project:prj_EXAMPLE1, ...]       # exact set
fields:                          # selected values; core fields start with _
  example.project:prj_EXAMPLE1: { _name: demo, region: eu-west-1 }
findings:                        # exact set: finding id -> state
  public_database:example.database:prj_EXAMPLE1/main: exposed
fixes: { "public_database:...": "exact fix text" }
checks: ["example.project|||ok", "example.database|prj_EXAMPLE1||error:permission"]   # kind|scope|rule|status
forbidden: [MURETTO_FAKE_SECRET] # must never appear in the scan result
```

Include at least one failure scenario (403/500) and put a
`MURETTO_FAKE_SECRET` marker in every field your `strip` list should remove.

Extract cases:

```yaml
- name: CLI db create
  tool: Bash
  input: { command: "example db create main --project prj_EXAMPLE1" }
  output: "Created database main in project prj_EXAMPLE1"
  expect:
    - { kind: example.database, value: main, container: prj_EXAMPLE1, role: created }
- name: CLI db list is read-only
  tool: Bash
  input: { command: "example db list" }
  output: "main  prj_EXAMPLE1"
  expect: []
```

Cover every creation path from your research (CLI text and JSON modes, MCP,
API via curl), failed calls, and read-only calls (`expect: []` or
`referenced`).

A case for a `created` signature must also say which scan-fixture
resource it creates, which proves attribution finds it (the match rule, the
container and the id shape all have to agree):

```yaml
- name: CLI db create
  tool: Bash
  input: { command: "example db create main --project prj_EXAMPLE1" }
  output: "Created database main in project prj_EXAMPLE1"
  expect:
    - { kind: example.database, value: main, container: prj_EXAMPLE1, role: created }
  matches: [example.database:prj_EXAMPLE1/main]
```

Run your adapter's tests offline:

```sh
muretto adapter test adapters/example/adapter.yaml --strict
# from a clone:
make adapter-test P=example
```

It finds `adapters/example/testdata/`, runs every scan scenario through the
broker's own checks (answered locally, never the network) and every extract
case, and with `--strict` applies the **shipping rules** CI enforces for every
adapter in `adapters/` (`internal/adaptercheck`, `TestShippedAdapters`):

- at least one scan scenario, and one **failure scenario** whose
  `expect.yaml` lists an error check (an endpoint answering 403 or 500);
- if any endpoint has `strip`, a scenario with `MURETTO_FAKE_SECRET` in its
  bodies and under `forbidden`;
- every signature exercised by at least one extract case;
- every `created` signature exercised by a case with `matches:`;
- at least one extract case that proves nothing (`expect: []`).

A pull request with only a YAML file fails. `--openapi spec.json` adds a dry
run against fake responses built from the spec. Generated adapters can start
from `muretto adapter new spec.json --fixtures adapters/example/testdata`,
which writes a passing starter set (a basic scan, a failure scan and an
extract case per signature) from fake bodies. Replace the generated values with
the shapes and examples from the provider's docs (or recorded, sanitized
responses, if you have a test account), and say in `research.md` which they are.
Hand-built fixtures are fine for a pull request; [FIXTURES.md](FIXTURES.md) lists
the fake values to use.

Then run `go test ./adapters/ ./internal/adapters ./internal/adaptercheck ./internal/broker`
(`go test ./adapters/` is the gate CI runs on every adapter). The suite loads every
adapter, runs every scan scenario through the real broker against an
`httptest` server, fails if any request falls outside the allowlist or has no
fixture, runs every extract case, scans all fixtures for secrets, and applies
the shipping rules above.

When you have a **test** account, record real shapes through the broker:
`MURETTO_LIVE=1 MURETTO_<PROVIDER>_TOKEN=… go test ./internal/adapters -run TestRecordLive -v`
(see [FIXTURES.md](FIXTURES.md)). Recordings land in the git-ignored
`adapters/<provider>/testdata/recorded/`; review them before moving anything
under `scan/`.

## 10. Checklist

- [ ] Research doc with sources and dates; nothing invented.
- [ ] Only GET endpoints you need; secret fields under `strip`.
- [ ] Attributes: the minimum, no secrets, null vs missing preserved.
- [ ] Least-privilege `token_help`, honest about what the token can do.
- [ ] Every rule tri-state; `caveat` says what the API cannot see.
- [ ] Signatures for every documented creation path; reads are `referenced`.
- [ ] `container` only when the creating call named it.
- [ ] Fixtures: happy path, failures, stripping markers, extract cases.
- [ ] `muretto adapter test adapters/<provider>/adapter.yaml --strict` passes.
- [ ] `coverage` lists every kind, including planned ones.

## 11. Official adapters: changes outside the folder

The YAML, research and fixtures are enough for an adapter to load, scan and
pass CI. An adapter that ships as official also needs a few small changes
outside `adapters/`. They are reviewed as security changes, and a maintainer
can make them for you when your pull request lands:

| File | What goes there |
|---|---|
| [`internal/broker/pinned.go`](../internal/broker/pinned.go) | `deniedPaths`: every endpoint that returns secret values, source code or data, even though the adapter never lists it. A POST body (`pinned_body`) is also declared here. |
| [`internal/redact/redact.go`](../internal/redact/redact.go) (plus its test) | The provider's token formats, so a token an agent prints never reaches the spool. |
| `apps/dashboard/src/app/(app)/providers/_components/derive.ts` | The dashboard's Providers page: move the provider from `COMING` to `SUPPORTED`, and add its `ENV`, `LISTS` and `FLAGS` entries. |
| [`README.md`](../README.md) | The "Supported today" table. |

Note the Go changes in [`docs/SECURITY.md`](../docs/SECURITY.md).

