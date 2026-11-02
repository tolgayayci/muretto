# Security

## Reporting a vulnerability

Please don't open a public issue.

- **Preferred:** GitHub private vulnerability reporting. Open <https://github.com/tolgayayci/muretto/security/advisories/new> (or **Security → Report a vulnerability** on the repository).
- **Fallback:** email [tolga@yk-labs.com](mailto:tolga@yk-labs.com).

Include what you found, how to reproduce it, and which version or commit you used (`muretto version`). We aim to reply within 3 working days. Please give us a reasonable time to release a fix before you disclose it publicly.

## Supported versions

Muretto is pre-1.0 and has no tagged release yet. Security fixes go to the `main` branch and, once releases exist, to the latest release only.

| Version | Supported |
|---|---|
| `main` | yes |
| latest `0.x` release (from v0.1.0) | yes |
| older `0.x` releases | no |

## Security model (summary)

Muretto is built so that it cannot change your infrastructure or leak your secrets:

- **Read-only, enforced in code.** Every provider request goes through one module with an explicit allowlist of read endpoints. Anything else fails before it leaves your machine, and tests prove it.
- **No actions.** Muretto only shows and flags. It has no command that changes, deletes or suggests changes to anything in your accounts.
- **Secrets redacted twice:** when data is recorded and again when it's shown or synced.
- **Tokens live in your OS keychain**, never in plain files, logs, spool files or sync payloads.
- **Local-first.** Everything works without an account. Sync is opt-in, and the CLI shows exactly what will be sent first.
- **Hooks never block your agent.** They write one small file locally and exit, with no network access.
- **No telemetry.**

The full model, including where each guarantee is enforced, is in [`docs/SECURITY.md`](docs/SECURITY.md). If the code changes, that document changes in the same pull request.
