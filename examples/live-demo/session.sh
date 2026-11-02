#!/usr/bin/env bash
# examples/live-demo/session.sh <dev>: one headless Claude Code session for the demo grid.
#
# Started by run.sh in a tmux pane (or in the background with run.sh --wait).
# It waits for the shared "go" file, then runs `claude -p` with Muretto's
# hooks from the per-run settings file, and pretty-prints the stream. The
# Supabase MCP server comes from the workdir's .mcp.json (written by run.sh,
# approved by enabledMcpjsonServers in the settings file), so hook events
# report it as project scope and Muretto can read its pinned project_ref.
#
# Demo tool for TEST accounts only. Muretto itself never writes anything.
set -euo pipefail
# shellcheck source=examples/live-demo/lib.sh
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

usage() { say "usage: examples/live-demo/session.sh [--dry-run] [--no-wait] <alice|bob|carol|dan|eve>"; }
NO_WAIT=0
DEV=''
while [ $# -gt 0 ]; do
	case $1 in
	--dry-run) DRY_RUN=1 ;;
	--no-wait) NO_WAIT=1 ;;
	-h | --help) usage && exit 0 ;;
	-*) usage && exit 2 ;;
	*) DEV=$1 ;;
	esac
	shift
done
case " $DEMO_DEVS " in *" $DEV "*) [ -n "$DEV" ] ;; *) false ;; esac || { usage; exit 2; }

load_env
require_demo
clean_claude_env

# ---------------------------------------------------------------- tasks
# Titles are what `-n` shows and what the dashboard calls the session.
# Prompts name every resource with the demo- prefix (or an entry of
# DEMO_TABLES) so reset.sh can find and remove exactly what they create.

REF=$(dev_ref "$DEV")
case $DEV in
alice)
	TITLE="Add a waitlist signup"
	PROMPT="Add a waitlist signup to our landing page in ./demo-waitlist.
1. Create a Supabase table public.waitlist (id bigint identity primary key, email text unique not null, created_at timestamptz default now()) using the Supabase execute_sql tool. Enable row level security on it and add one policy that lets the anon role insert, nothing else.
2. Add a small email signup form to demo-waitlist/index.html that inserts into that table with supabase-js from a CDN (get the project URL and publishable key with the Supabase tools).
3. Deploy a Vercel preview: run exactly \`cd demo-waitlist && vercel deploy --yes --scope $VERCEL_TEAM_SLUG\`. The Vercel CLI reads the token from the environment; never print it.
Finish with one line: the preview URL."
	TOOLS="mcp__supabase__execute_sql|mcp__supabase__get_project_url|mcp__supabase__get_publishable_keys|Read|Edit|Write|Bash(cd *)|Bash(vercel *)|Bash(ls *)"
	;;
bob)
	# This prompt must produce a public.customers table with RLS off and seed
	# rows on every run. It hands the agent the exact SQL and allows only
	# execute_sql, so nothing (list_tables advisories, other tools) nudges it
	# to change the SQL. RLS off is simply Postgres' default for SQL-created
	# tables. README.md discloses this.
	TITLE="Prototype a customer import"
	PROMPT="Prototype a customer import. Run this SQL exactly as written, in one call, with the Supabase execute_sql tool, then reply with one line saying how many rows are in public.customers. Don't run anything else.

create table public.customers (
  id bigint generated always as identity primary key,
  name text not null,
  email text not null,
  company text,
  plan text not null default 'trial',
  created_at timestamptz not null default now()
);
insert into public.customers (name, email, company, plan) values
  ('Ada Example', 'ada@example.com', 'Example Labs', 'pro'),
  ('Ben Example', 'ben@example.com', 'Example Foods', 'trial'),
  ('Cleo Example', 'cleo@example.com', 'Example Health', 'enterprise'),
  ('Dev Example', 'dev@example.com', 'Example Robotics', 'pro'),
  ('Eli Example', 'eli@example.com', 'Example Travel', 'trial');"
	TOOLS="mcp__supabase__execute_sql"
	;;
carol)
	TITLE="Add image uploads"
	PROMPT="Add image uploads. Create a private Supabase Storage bucket with id and name demo-uploads (5 MB file size limit, allowed MIME types image/png, image/jpeg and image/webp) by inserting it into storage.buckets with the Supabase execute_sql tool. Then add a policy on storage.objects that lets authenticated users upload into that bucket only. Finish with one line naming the bucket."
	TOOLS="mcp__supabase__execute_sql|mcp__supabase__apply_migration"
	;;
dan)
	TITLE="Add a webhook handler"
	PROMPT="Add a webhook handler as a Supabase Edge Function named demo-webhook. Write it as a single index.ts that accepts POST requests with a JSON body, logs the event type, and returns {\"received\": true}; other methods get 405. Deploy it with the Supabase deploy_edge_function tool, with verify_jwt false because the payment provider calls it without a user token. Finish with one line: the function slug."
	TOOLS="mcp__supabase__deploy_edge_function|mcp__supabase__list_edge_functions|Read|Write"
	;;
eve)
	TITLE="Deploy a staging landing page"
	PROMPT="Deploy a staging copy of our landing page in ./demo-landing-staging to Vercel. Check that index.html exists, then run exactly \`cd demo-landing-staging && vercel deploy --yes --scope $VERCEL_TEAM_SLUG\`. The Vercel CLI reads the token from the environment; never print it. Finish with one line: the deployment URL."
	TOOLS="Read|Bash(cd *)|Bash(vercel *)|Bash(ls *)"
	;;
esac

SID_FILE=$DEMO_RUN_DIR/devs/$DEV/session-id
if [ "$DRY_RUN" = 1 ] && [ ! -f "$SID_FILE" ]; then
	SID=00000000-0000-4000-8000-000000000000
else
	[ -f "$SID_FILE" ] || die "no session id at $SID_FILE; start the demo with examples/live-demo/run.sh"
	SID=$(cat "$SID_FILE")
fi
HOOKS=$DEMO_RUN_DIR/hooks.json
LOG=$DEMO_RUN_DIR/logs/$DEV.jsonl
WORK=$(dev_workdir "$DEV")

# TOOLS is |-separated (rules like "Bash(cd *)" contain spaces).
TOOL_ARGS=()
while IFS= read -r t; do TOOL_ARGS+=("$t"); done < <(printf "%s\n" "$TOOLS" | tr "|" "\n")
CMD=(claude -p "$PROMPT"
	--model "$DEMO_MODEL"
	--output-format stream-json --verbose --include-hook-events
	--permission-mode dontAsk
	--allowedTools "${TOOL_ARGS[@]}"
	--settings "$HOOKS"
	--session-id "$SID"
	-n "$DEV: $TITLE"
	--max-budget-usd "$DEMO_MAX_BUDGET_USD")

# What the session (and its hooks) see. MURETTO_HOME gives each developer
# their own Muretto store; Claude Code passes it on to the hook processes.
export MURETTO_HOME
MURETTO_HOME=$(dev_home "$DEV")
# The Vercel CLI reads VERCEL_TOKEN from the environment, so prompts never
# contain "$VERCEL_TOKEN": Claude Code's dontAsk mode denies any command with a
# variable expansion even when its prefix is allowed (seen live 2026-10-27).
export SUPABASE_ACCESS_TOKEN VERCEL_TOKEN VERCEL_TEAM_SLUG
export VERCEL_TELEMETRY_DISABLED=1

if [ "$DRY_RUN" = 1 ]; then
	say "# $DEV ($REF): cd $WORK"
	show_cmd env "MURETTO_HOME=$MURETTO_HOME" "${CMD[@]}"
	say "    < /dev/null | tee $LOG | jq --unbuffered -R -r -f $DEMO_DIR/pretty.jq"
	exit 0
fi

printf '\033]2;%s\007' "$DEV: $TITLE"
printf '%s%s%s · %s\n' "$C_B" "$(dev_name "$DEV")" "$C_0" "$TITLE"
printf '%s$ claude -p "%s…" -n "%s: %s"%s\n\n' "$C_DIM" "$(printf '%s' "$PROMPT" | head -n 1 | cut -c1-48)" "$DEV" "$TITLE" "$C_0"

if [ "$NO_WAIT" != 1 ]; then
	n=0
	until [ -f "$DEMO_RUN_DIR/go" ]; do
		sleep 0.2
		n=$((n + 1))
		[ "$n" -lt 9000 ] || die "timed out waiting for the start signal"
	done
fi

mkdir -p "$(dirname "$LOG")"
cd "$WORK"
status=0
"${CMD[@]}" </dev/null 2>>"$DEMO_RUN_DIR/logs/$DEV.stderr" | tee "$LOG" | jq --unbuffered -R -r -f "$DEMO_DIR/pretty.jq" || status=$?
[ "$status" = 0 ] || say "${C_RED}session exited with status $status${C_0} (stderr: $DEMO_RUN_DIR/logs/$DEV.stderr)"
exit "$status"
