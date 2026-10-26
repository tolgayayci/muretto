# Security

## Reporting a vulnerability

Please don't open a public issue. Use GitHub's private vulnerability reporting: on this repository, go to **Security → Report a vulnerability**. We aim to reply within 3 working days.

## Security model (summary)

Muretto is built so that it cannot harm your infrastructure or leak your secrets:

- **Read-only, enforced in code.** Every provider request goes through one module with an explicit allowlist of read endpoints. Anything else fails before it leaves your machine, and a test proves it.
- **No actions.** Muretto prints the command or SQL a human can run to fix something. It never runs it.
- **Secrets redacted twice:** when data is recorded and again when it's shown or synced.
- **Tokens live in your OS keychain**, never in plain files, logs, spool files or sync payloads.
- **Local-first.** Everything works without an account. Sync is opt-in, and the CLI shows exactly what will be sent first.
- **Hooks never block your agent.** They write one small file locally and exit, with no network access.

The full model, including how each guarantee is enforced, is in [`docs/SECURITY.md`](docs/SECURITY.md). If the code changes, that document changes in the same pull request.
