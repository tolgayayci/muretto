# Muretto demo harness

One command runs the real demo behind the 60-second video (`SCRIPT.md`): five headless Claude Code sessions (alice, bob, carol, dan, eve) build things in **test** Supabase and Vercel accounts while Muretto's hooks record them and a local dashboard fills up live.

> **This harness is a demo tool for test accounts only. It is not part of Muretto.** The agents and these scripts create and delete real resources. **Muretto itself never writes**: it only reads, through its broker allowlist.

## What is here

| File | What it does |
|---|---|
| `.env.example` | Every variable, documented. Copy to `demo/.env` (git-ignored). |
| `lib.sh` | Shared guards: `MURETTO_DEMO=1`, typed confirmation of the test project ref, tool checks with install hints (never installs), unsets inherited `CLAUDECODE` / `CLAUDE_CODE_*`, dry-run printing with secrets masked. |
| `setup.sh` | One-time test-account prep (dry run by default, `--apply` to run). |
| `run.sh` | The demo: local server + dashboard, five developers, tmux 2×3 grid. `--dry-run` prints every command. |
| `session.sh` | One headless `claude -p` session (used by `run.sh` in each pane). |
| `syncloop.sh` | The sixth pane: start signal, `muretto sync now` per developer, status line. |
| `pretty.jq` | Turns `stream-json` into readable pane output; masks token-shaped strings. |
| `prove.sh` | The curl with only the public key, then `muretto why/fix customers` as bob. |
| `reset.sh` | Deletes what the demo created (dry run by default, `--apply` to run). |
| `selftest.sh` | Offline check of the local plumbing. No accounts, no network beyond 127.0.0.1. |
| `SCRIPT.md` | Shot list, disclosure, run checklist, recording tips, the 10-run reliability loop. |

## Prerequisites

**Test accounts (never your real ones):**

- A dedicated **Supabase** org with 1 or 2 **pre-created** projects. Don't create projects live: it is slow and plan-limited. Each project's name must contain `demo` or `test`; the scripts check it (`GET /v1/projects`) and refuse otherwise.
  - Free plan: 2 active free projects per user, counted across every org where you are Owner or Admin; paused projects don't count. Free projects pause after a week of inactivity. Branching needs Pro (the demo doesn't use branches). Source: `docs/research/supabase.md` Q8.
  - A personal access token (`sbp_…`) of a member of that org. The agents' Supabase MCP server and `setup.sh` / `reset.sh` use it; it can write.
  - Optional but better: a second, read-scoped token for Muretto (`MURETTO_SUPABASE_TOKEN`). Muretto's broker keeps reads read-only either way.
  - The primary project's **publishable** key (`sb_publishable_…`) for `prove.sh`. Each project's **secret** key only if you want `reset.sh` to delete the `demo-uploads` bucket (the Management API has no bucket delete).
- A dedicated **Vercel** team whose slug contains `demo` or `test`, and a team-scoped token (`vcp_…`). Vercel has no read-only token (`docs/research/vercel.md` Q2).
  - Hobby: 1 concurrent build (alice's and eve's deploys queue, which is fine but slower on video), non-commercial use only. A Pro trial avoids both (`docs/research/vercel.md` Q8).

**Tools** (the scripts check and print install hints; they never install anything): Claude Code (`claude`, logged in), `tmux`, `jq`, `curl`, Go 1.27.3+, Node/npm (dashboard), `uuidgen`, the Vercel CLI (`vercel`, used by the agents), and `muretto` built from this repo (`make build` → `bin/muretto`; the hooks point at it). The scripts are bash 3.2 compatible (macOS default).

## The one-command flow

```sh
cp demo/.env.example demo/.env && chmod 600 demo/.env   # fill it in, set MURETTO_DEMO=1
make build && (cd web && npm ci)
demo/selftest.sh                 # offline: server, dev tokens, hooks -> spool -> sync
demo/setup.sh                    # read the dry run
demo/setup.sh --apply            # once per test project (see "Why setup.sh" below)

demo/run.sh --dry-run            # every command, nothing run
demo/run.sh                      # type the test ref; tmux attaches
#   open the printed dashboard link (signs in as alice), switch to Team,
#   press Enter in the bottom-right pane to start all five sessions
demo/prove.sh                    # curl with the public key, then why/fix as bob
demo/reset.sh --apply            # list, confirm, delete; also stops everything local
```

What `run.sh` does, in order:

1. Refuses unless `MURETTO_DEMO=1`, every project name contains `demo`/`test`, and you type the primary project ref.
2. Builds and starts `murettod` with `MURETTO_DEV_LOGIN=1` on `127.0.0.1:$DEMO_SERVER_PORT`, in-memory (stopping it clears all server data), and the dashboard on `localhost:$DEMO_WEB_PORT` with `MURETTO_API_URL` pointing at it.
3. For each developer: their own `MURETTO_HOME` under `demo/.run/devs/<dev>/home`, a CLI token from the dev-only `POST /v1/dev/token` (stored 0600, local server only), `muretto init --yes --no-hooks` (connects the test accounts from `MURETTO_SUPABASE_TOKEN` / `MURETTO_VERCEL_TOKEN`), `sync.server` set to the local server, `muretto sync on --yes`.
4. alice creates the team `DEMO_ORG_NAME`; bob, carol, dan and eve join with invites (dev cookie login + CSRF, the same API the dashboard uses).
5. Writes `demo/.run/hooks.json` (Muretto's six hook events in exec form, pointing at the repo-built `bin/muretto`, plus `enabledMcpjsonServers: ["supabase"]`; passed with `--settings`, so your own Claude Code settings are never touched), and a `.mcp.json` in each developer's workdir with the Supabase MCP server pinned to that developer's test project (`?project_ref=<ref>`; the token is `"Authorization": "Bearer ${SUPABASE_ACCESS_TOKEN}"`, expanded by Claude Code, never written to disk).
6. Creates the tmux grid. Each pane runs `claude -p` in its workdir with `--settings`, `--session-id`, `-n "<dev>: <task>"`, `--permission-mode dontAsk --allowedTools …` (only what that task needs), `--max-budget-usd`, `--output-format stream-json --verbose --include-hook-events`, piped through `pretty.jq`. Never `--bare` (it would skip hooks).

**Why `.mcp.json` and not `--mcp-config`.** A server pinned to one project takes no `project_id` in its tool calls, so Muretto learns the project from the server's config: the hook reads the server's `project_ref` from `.mcp.json` (project scope) and records only that (decision 0905). Claude Code doesn't say where a `--mcp-config` server came from (`source: "dynamic"`), so those sessions would show every Supabase resource as "creator unknown". Without `--strict-mcp-config`, your own user-scope MCP servers (from `~/.claude.json`) also load in the panes; `--allowedTools` still limits each agent to its task's tools, so they can't be called.

Each developer's muretto reads only its own session's transcripts: `CLAUDE_CONFIG_DIR` for muretto points at `demo/.run/devs/<dev>/claude`, which holds a symlink to that developer's Claude Code project directory only. Without this, every developer would also pick up the other four sessions and your personal Claude Code history.

### Why setup.sh

Since Oct 30, 2026, Supabase no longer grants new `public` tables to `anon`/`authenticated`. Bob's RLS-off table would then return `42501` to the curl, and Muretto would correctly not flag it. `setup.sh --apply` restores the pre-change default privileges **on the test project only** (option (a) in `docs/OPEN_QUESTIONS.md` §5; undo statement in the script). `SCRIPT.md` discloses it on screen. It also prints the steps to set the Vercel **test** team's default deployment protection to None (§5b).

## Troubleshooting

| Symptom | Fix |
|---|---|
| `refusing to run: MURETTO_DEMO is not 1` | Set it in `demo/.env` (the file wins over your shell). |
| `project … is named '…'` refusal | Rename the test project to include `demo` or `test`, or use another project. |
| `missing: tmux` (or another tool) | Install it with the printed hint; the scripts never install anything. |
| `a previous demo is still running` | `demo/reset.sh --local --apply` (local only, no provider calls). |
| `something already answers on http://127.0.0.1:8080` | Another server is running; stop it or change `DEMO_SERVER_PORT`. |
| `web/node_modules is missing` | `(cd web && npm ci)`, or run with `--no-dashboard`. |
| Dashboard shows nothing | Use the printed link (it signs in through the dashboard's `/api` proxy so the cookie is same-origin), switch to **Team**, and check pane 6 shows ✓ for each developer. Server log: `demo/.run/server.log`. |
| A pane shows `mcp: supabase=failed` | Check `SUPABASE_ACCESS_TOKEN`, and that the project ref is right. `demo/.run/logs/<dev>.stderr` has the details. |
| A pane says a tool was denied | That task's `--allowedTools` list in `session.sh` lacks it. Add the narrowest rule that works. |
| `prove.sh` gets HTTP 401/403 or `42501` | 42501: `setup.sh --apply` was not run on that project. 401: wrong publishable key or project ref. |
| Bob's table has RLS on | The agent changed the SQL. Read `demo/.run/logs/bob.jsonl`; measure with the 10-run loop in `SCRIPT.md`. |
| Alice's/eve's preview needs a Vercel login | The test team's default protection is still on (setup.sh step 2). |
| Alert doesn't turn green | Pane 6 forces a fresh check for bob every `DEMO_REFRESH_INTERVAL` s; Muretto otherwise caches scans for 60 s. |
| `reset.sh` can't delete the bucket | Set `SUPABASE_SECRET_KEY` (and `_2`) or delete `demo-uploads` in the dashboard. |
| Hooks didn't fire | Panes show `⚑ muretto hooks active` at start. If not: `bin/muretto` missing (run `make build`), or `demo/.run/devs/<dev>/home/hook-errors.log`. |

## Unverified (check on the first real run)

- `--allowedTools` permission rules with arguments (`Bash(vercel *)`) in `dontAsk` mode; the research verified exact tool names only.
- `enabledMcpjsonServers` from a `--settings` file approving a workdir's `.mcp.json` server in `-p` mode (observed once in a live test, with `source: "project"` in the hook input).
- The raw Storage API paths `POST /storage/v1/bucket/{id}/empty` and `DELETE /storage/v1/bucket/{id}` (from supabase storage-js; `docs/research/supabase.md` Q9 marks them unverified).
- Whether the Vercel team settings page offers a team-wide default for deployment protection on your plan.
- `GET /v10/projects?search=` matching by name prefix; `reset.sh` filters by the `demo-` prefix again anyway.
