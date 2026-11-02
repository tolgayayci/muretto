# Adapters

An adapter teaches Muretto one provider. It is a declarative, read-only YAML
file that says:

- which GET endpoints Muretto may call to list what exists (this list is the
  broker's allowlist: nothing else can be requested);
- which response fields to keep, and which to strip because they can hold
  secrets;
- how to recognise an agent creating something (CLI output, MCP results, curl
  against the API), so a resource can be traced to the session that made it;
- which exposure rules apply (a public bucket, an open preview), each one
  `exposed`, `not_exposed` or `unknown`.

Muretto only lists and flags. An adapter can never write, delete or change
anything in a provider account.

## Layout

One folder per provider. The folder name is the provider name.

```
adapters/
  README.md            this page
  WRITING.md           the full format reference
  STANDARDS.md         what an official adapter must contain, and how it is reviewed
  FIXTURES.md          the fake values fixtures use, and the live recorder
  _template/
    adapter.yaml       every field with a comment; copy it to start
  <provider>/
    adapter.yaml       the adapter
    research.md        sources (URL and date) for every endpoint, field and output string
    coverage.md        every resource kind the provider has: full, partial or planned
    testdata/
      scan/<scenario>/   routes.yaml, expect.yaml and JSON response bodies
      extract/cases.yaml tool calls and the identifiers they should yield
      recorded/          live recorder output, git-ignored
  adapters.go          embeds */adapter.yaml into the binary
  adapters_test.go     the gate CI runs
```

Shipped today: [supabase](supabase/), [vercel](vercel/), [github](github/),
[cloudflare](cloudflare/), [neon](neon/), [stripe](stripe/),
[render](render/), [planetscale](planetscale/). Folders starting with `_`
are not loaded.

## Add a provider in 5 steps

You need Go (the version in `go.mod`) and a clone. No provider account is
needed. The examples use a provider called `example`.

**1. Research.** Read the provider's API docs and write
`adapters/example/research.md`: the list endpoints, the fields you will keep,
the narrowest read-only token, how agents create resources (CLI, MCP, API) with
the exact output, and the endpoints that return secrets. Cite a URL and date
for each. Never guess an endpoint or an output string.

**2. Draft the adapter.** With an OpenAPI file:

```sh
mkdir -p adapters/example
go run ./cmd/muretto adapter new openapi.json --provider example \
  -o adapters/example/adapter.yaml --fixtures adapters/example/testdata
```

This writes a draft and a passing starter set of fixtures. Without a spec,
copy the template:

```sh
mkdir -p adapters/example
cp adapters/_template/adapter.yaml adapters/example/adapter.yaml
```

Then edit it: set `provider: example`, review every line marked `REVIEW`, and
add CLI signatures from your research (OpenAPI cannot describe CLI output).
Add a `coverage.md` listing every kind.

**3. Write fixtures** in `adapters/example/testdata/`:

- `scan/basic/`: what a normal account returns;
- `scan/failures/`: at least one endpoint answering 403 or 500;
- `extract/cases.yaml`: one case per signature, plus one that proves nothing
  (a failed or read-only call, `expect: []`).

Build the response bodies from the provider's documented response shapes and
examples, with the fake values in [FIXTURES.md](FIXTURES.md). Put
`MURETTO_FAKE_SECRET` in every field your `strip` list removes. Hand-built
fixtures are fine; you do not need a test account.

**4. Test offline.**

```sh
make adapter-test P=example
# the same as:
go test -count=1 ./adapters/ -v
go run ./cmd/muretto adapter test adapters/example/adapter.yaml --strict
```

To try it against your own account (read-only), `make build` and then
`bin/muretto adapter install adapters/example/adapter.yaml` and
`bin/muretto init`.

**5. Open a pull request** with the "Adapter" template:
`?template=adapter.md` on the compare page, or copy the checklist from
[`.github/PULL_REQUEST_TEMPLATE/adapter.md`](../.github/PULL_REQUEST_TEMPLATE/adapter.md).

## What CI checks

`go test ./adapters/` is the gate. For every folder with an `adapter.yaml` it
checks that:

- the adapter loads and validates, and its `provider` matches the folder name;
- every scan scenario passes through the real broker against a local fake
  API: no request outside the allowlist, the exact resources, findings and
  checks in `expect.yaml`;
- there is at least one scan scenario and one failure scenario;
- if any endpoint strips fields, a scenario puts `MURETTO_FAKE_SECRET` in them
  and lists it under `forbidden`;
- every signature has an extract case, every `created` signature has a case
  whose `matches:` names the fixture resource it creates, and at least one
  case proves nothing;
- an adapter with no fixtures fails.

The rest of the Go suite (`go test ./...`) also scans every file under
`adapters/` for secrets, and the `secrets` job runs gitleaks over the whole
pull request.

## Community and official adapters

A pull request that adds a folder here and passes CI is all a contributor
needs to send. Nothing else in the repo has to change for the adapter to load
and scan.

To ship as **official**, an adapter also meets [STANDARDS.md](STANDARDS.md),
is verified read-only against a live test account, and gets a few changes
outside this folder: the broker's deny list, the token formats for redaction,
the dashboard's provider list and the README table (see
[WRITING.md section 11](WRITING.md#11-official-adapters-changes-outside-the-folder)).
Maintainers can make those for you. Until it is verified live, an adapter's
header says `# Not yet verified live.`

You can also keep an adapter to yourself: `muretto adapter install` puts it in
`~/.muretto/adapters/`, where it may only use GET.

## Read next

- [WRITING.md](WRITING.md): every field, template, predicate and signature option.
- [STANDARDS.md](STANDARDS.md): naming, secrets, severities and the review gate.
- [FIXTURES.md](FIXTURES.md): fake values and recording from a test account.
- [`_template/adapter.yaml`](_template/adapter.yaml): the starting point.
