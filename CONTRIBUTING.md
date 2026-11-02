# Contributing to Muretto

Thanks for helping. Muretto is a read-only tool: it never creates, changes or deletes anything in a provider account. Every contribution has to keep that promise.

## Quick start

```sh
git clone https://github.com/tolgayayci/muretto
cd muretto
make setup   # installs the git hooks (secret scanning before every commit)
make test    # the whole suite: no network, no accounts needed
```

## Ground rules

- **Read-only.** Provider network calls go only through `internal/broker`, which allows an explicit list of read endpoints. Don't add a request path anywhere else.
- **No secrets, ever.** Not in code, fixtures, docs, screenshots or commit messages. The pre-commit hook and CI run a secret scanner. Fixtures in `adapters/testdata` must be sanitized (fake ids, emails, keys and URLs).
- **No real account data.** Use obvious fakes such as `example-org` and `abcdefghijklmnopqrst`.
- **Honest output.** A label is `exact` only with stored evidence. Unknown is never shown as `$0`.
- **Few dependencies.** Justify each new dependency in your pull request.

## Adding a provider

A provider is one YAML file in `adapters/`, plus fixtures and scenarios. No Go changes. See [`docs/WRITING_AN_ADAPTER.md`](docs/WRITING_AN_ADAPTER.md). Have an OpenAPI file? `muretto adapter new openapi.json --fixtures adapters/testdata/<provider>` drafts both.

CI rejects an adapter that does not pass its own fixtures and the shipping rules (a failure scenario, secret markers for stripped fields, an extract case per signature). Check locally with `muretto adapter test adapters/<provider>.yaml --strict`.

## Live tests

Tests against real **test** accounts are opt-in and off by default. They are never run in CI for pull requests from forks.

## Decisions

Non-obvious design decisions are recorded in `docs/DECISIONS.md` and `docs/decisions/`. If your change makes a new one, add a short record.

## Conduct and contact

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md). Report security issues privately as described in [SECURITY.md](SECURITY.md). Contact address for conduct reports: to be announced before the public launch.
