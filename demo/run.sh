#!/usr/bin/env bash
# demo/run.sh: the one-command Muretto demo.
#
# Starts the local sync server (dev login, 127.0.0.1, in-memory) and the
# dashboard, gives alice, bob, carol, dan and eve each their own MURETTO_HOME
# and dev token with sync on, joins them into one team, then launches five
# headless Claude Code sessions in a tmux 2x3 grid. The sixth pane syncs every
# few seconds and shows a status line. When alice or eve run, their Vercel
# projects first get one placeholder production deploy (a project's first
# deploy always goes to production; the agents' deploys must be previews).
#
#   demo/run.sh --dry-run        print every command, run nothing
#   demo/run.sh                  the real demo (asks you to type the test ref)
#   demo/run.sh --only bob --wait
#                                no tmux: run bob only, wait, sync, exit
#                                (the 10-run reliability loop in SCRIPT.md)
#
# Demo tool for TEST accounts only. The agents create real resources in the
# test accounts; demo/reset.sh removes them. Muretto itself never writes.
set -euo pipefail
# shellcheck source=demo/lib.sh
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

usage() {
	sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//'
	say "flags: --dry-run  --only <dev,dev>  --wait  --no-dashboard  --no-attach  --autostart"
}

ONLY=$DEMO_DEVS
WAIT=0 NO_WEB=0 NO_ATTACH=0 AUTOSTART=0
while [ $# -gt 0 ]; do
	case $1 in
	--dry-run) DRY_RUN=1 ;;
	--only)
		[ $# -ge 2 ] || die "--only needs a list, e.g. --only bob,carol"
		ONLY=$(printf '%s' "$2" | tr ',' ' ')
		shift
		;;
	--wait) WAIT=1 NO_WEB=1 NO_ATTACH=1 AUTOSTART=1 ;;
	--no-dashboard) NO_WEB=1 ;;
	--no-attach) NO_ATTACH=1 ;;
	--autostart) AUTOSTART=1 ;;
	-h | --help) usage && exit 0 ;;
	*) usage && exit 2 ;;
	esac
	shift
done
for d in $ONLY; do
	case " $DEMO_DEVS " in *" $d "*) ;; *) die "unknown developer '$d' (choose from: $DEMO_DEVS)" ;; esac
done
export DRY_RUN

load_env
require_demo
clean_claude_env

step "Checking tools"
TOOLS="claude jq curl go muretto uuidgen"
case " $ONLY " in *" alice "* | *" eve "*) TOOLS="$TOOLS vercel" ;; esac
[ "$WAIT" = 1 ] || TOOLS="$TOOLS tmux"
[ "$NO_WEB" = 1 ] || TOOLS="$TOOLS npm"
# shellcheck disable=SC2086 # word list
check_tools $TOOLS
if [ "${READ_TOKEN_REUSED:-0}" = 1 ]; then
	warn "MURETTO_SUPABASE_TOKEN is empty: Muretto will read with SUPABASE_ACCESS_TOKEN. A read-scoped token is better (README)."
fi

if [ "$DRY_RUN" != 1 ]; then
	if [ -f "$DEMO_RUN_DIR/server.pid" ] || { [ "$WAIT" != 1 ] && tmux has-session -t "$TMUX_SESSION" 2>/dev/null; }; then
		die "a previous demo is still running or was not cleaned up. Run: demo/reset.sh --local --apply"
	fi
fi

step "Checking that the Supabase projects are test projects"
assert_test_projects
confirm_ref "This starts $(printf '%s' "$ONLY" | wc -w | tr -d ' ') Claude Code session(s) that CREATE real resources in the test accounts."

step "Preparing $DEMO_RUN_DIR"
run mkdir -p "$DEMO_RUN_DIR/logs"
[ "$DRY_RUN" = 1 ] || chmod 700 "$DEMO_RUN_DIR"
run rm -f "$DEMO_RUN_DIR/go"

# ---------------------------------------------------------------- Vercel seed
# A Vercel project's first deployment always goes to production, even with
# --target preview. alice and eve must produce a PREVIEW, so each of their
# projects first gets one placeholder production deploy, from a temp dir
# named like the project (outside the workdirs, so its .vercel/ link never
# reaches them; the agents' `vercel deploy --yes` links to the project by
# name). Projects that already have a READY production deployment are left
# alone.

# has_prod_deploy <name>: true if the test team has a project <name> with a
# READY production deployment (two reads: the projects list, then one page
# of that project's production deployments).
has_prod_deploy() {
	local name=$1 projects pid deps
	projects=$(api GET "$VERCEL_API/v10/projects?teamId=$VERCEL_TEAM_ID&search=$name&limit=100" VERCEL_TOKEN) ||
		die "could not list Vercel projects in $VERCEL_TEAM_SLUG"
	pid=$(printf '%s' "$projects" | jq -r --arg n "$name" '[.projects[]? | select(.name == $n) | .id][0] // empty')
	[ -n "$pid" ] || return 1
	deps=$(api GET "$VERCEL_API/v7/deployments?teamId=$VERCEL_TEAM_ID&projectId=$pid&target=production&state=READY&limit=1" VERCEL_TOKEN) ||
		die "could not list the deployments of Vercel project $name"
	[ "$(printf '%s' "$deps" | jq '[.deployments[]? | select(.readyState == "READY" or .state == "READY")] | length')" -gt 0 ]
}

# seed_html: the placeholder page of a seed deploy.
seed_html() {
	cat <<'HTML'
<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Placeholder</title></head>
<body><p>Placeholder production deploy for the Muretto demo (test account).</p></body>
</html>
HTML
}

# seed_project <name> <tmpdir>: one production deploy of the placeholder page
# into project <name>. The Vercel CLI reads VERCEL_TOKEN from the environment.
seed_project() {
	local name=$1 tmp=$2 dir log
	is_demo_name "$name" || die "refusing to seed '$name': not a $DEMO_PREFIX name"
	dir=$tmp/$name
	log=$DEMO_RUN_DIR/logs/seed-$name.log
	if [ "$DRY_RUN" = 1 ]; then
		api GET "$VERCEL_API/v10/projects?teamId=$VERCEL_TEAM_ID&search=$name&limit=100" VERCEL_TOKEN
		api GET "$VERCEL_API/v7/deployments?teamId=$VERCEL_TEAM_ID&projectId=prj_example&target=production&state=READY&limit=1" VERCEL_TOKEN
		say "    -> skip $name if it already has a READY production deployment; otherwise:"
		seed_html | write_file "$dir/index.html" 644
		show_cmd cd "$dir"
		say "    (VERCEL_TOKEN from the environment, VERCEL_TELEMETRY_DISABLED=1; output -> $log)"
		show_cmd vercel deploy --yes --prod --scope "$VERCEL_TEAM_SLUG"
		return 0
	fi
	if has_prod_deploy "$name"; then
		say "  Vercel project $name already has a production deployment ✓"
		return 0
	fi
	mkdir -p "$dir"
	seed_html >"$dir/index.html"
	say "  Vercel project $name: placeholder production deploy (log: $log)"
	(cd "$dir" && vercel deploy --yes --prod --scope "$VERCEL_TEAM_SLUG") >"$log" 2>&1
}

SEED=''
case " $ONLY " in *" alice "*) SEED="$SEED demo-waitlist" ;; esac
case " $ONLY " in *" eve "*) SEED="$SEED demo-landing-staging" ;; esac
if [ -n "$SEED" ]; then
	step "Seeding the Vercel projects with a production deploy, so the agents' deploys are previews:$SEED"
	export VERCEL_TELEMETRY_DISABLED=1
	# Outside the repo and the workdirs: no .vercel/ link can leak either way.
	SEED_TMP=${TMPDIR:-/tmp}
	SEED_TMP=${SEED_TMP%/}/muretto-seed.XXXXXX
	if [ "$DRY_RUN" = 1 ]; then
		run mktemp -d "$SEED_TMP"
	else
		SEED_TMP=$(mktemp -d "$SEED_TMP")
	fi
	seed_pids='' seed_failed=''
	for p in $SEED; do
		if [ "$DRY_RUN" = 1 ]; then
			seed_project "$p" "$SEED_TMP"
			continue
		fi
		seed_project "$p" "$SEED_TMP" &
		seed_pids="$seed_pids $!:$p"
	done
	for e in $seed_pids; do
		wait "${e%%:*}" || seed_failed="$seed_failed ${e#*:}"
	done
	run rm -rf "$SEED_TMP"
	if [ -n "$seed_failed" ]; then
		for p in $seed_failed; do
			[ ! -f "$DEMO_RUN_DIR/logs/seed-$p.log" ] || tail -n 20 "$DEMO_RUN_DIR/logs/seed-$p.log" >&2
		done
		die "seeding failed for:$seed_failed (logs above, in $DEMO_RUN_DIR/logs/). Without it, alice's/eve's deploys become Production, not Preview."
	fi
fi

start_server
[ "$NO_WEB" = 1 ] || start_web

# Every developer is set up (so the team is complete), even with --only.
for d in $DEMO_DEVS; do
	setup_dev "$d"
done
setup_org alice bob carol dan eve

step "Writing the per-run hooks settings"
hooks_settings | write_file "$DEMO_RUN_DIR/hooks.json" 600

step "Preparing each developer's working directory and session id"
for d in $DEMO_DEVS; do
	work=$(dev_workdir "$d")
	if [ "$DRY_RUN" = 1 ]; then
		sid=00000000-0000-4000-8000-00000000000$(printf '%s' "$DEMO_DEVS" | tr ' ' '\n' | grep -n "^$d\$" | cut -d: -f1)
	else
		sid=$(uuidgen | tr '[:upper:]' '[:lower:]')
	fi
	printf '%s\n' "$sid" | write_file "$DEMO_RUN_DIR/devs/$d/session-id" 600
	run git init -q -b "demo/$d" "$work"
	mcp_config "$(dev_ref "$d")" | write_file "$work/.mcp.json" 600
	case $d in
	alice) site=$work/demo-waitlist ;;
	eve) site=$work/demo-landing-staging ;;
	*) site='' ;;
	esac
	if [ -n "$site" ]; then
		write_file "$site/index.html" 644 <<'HTML'
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Acme</title></head>
<body style="font-family: system-ui, sans-serif; max-width: 40rem; margin: 4rem auto; padding: 0 1rem">
<h1>Acme</h1>
<p>The demo landing page. Nothing to see yet.</p>
</body>
</html>
HTML
	fi
done

# ---------------------------------------------------------------- --wait
if [ "$WAIT" = 1 ]; then
	step "Running without tmux: $ONLY"
	pids=''
	for d in $ONLY; do
		if [ "$DRY_RUN" = 1 ]; then
			DRY_RUN=1 "$DEMO_DIR/session.sh" --dry-run --no-wait "$d"
			continue
		fi
		"$DEMO_DIR/session.sh" --no-wait "$d" >"$DEMO_RUN_DIR/logs/$d.pane" 2>&1 &
		pids="$pids $!"
	done
	failed=0
	for p in $pids; do wait "$p" || failed=1; done
	for d in $ONLY; do
		muretto_as "$d" sync now >/dev/null || failed=1
	done
	[ "$DRY_RUN" = 1 ] || say "Sessions finished. Logs: $DEMO_RUN_DIR/logs/"
	exit "$failed"
fi

# ---------------------------------------------------------------- tmux grid
step "Launching the tmux grid ($TMUX_SESSION): 2 rows x 3 columns"
pane_cmd() { printf "bash %q %q; exec bash" "$DEMO_DIR/session.sh" "$1"; }
active() { case " $ONLY " in *" $1 "*) return 0 ;; *) return 1 ;; esac; }
cmd_for() {
	if active "$1"; then pane_cmd "$1"; else printf "echo %q; exec bash" "$1: not running (--only)"; fi
}
LOOP_CMD=$(printf "bash %q; exec bash" "$DEMO_DIR/syncloop.sh")
[ "$AUTOSTART" = 1 ] && LOOP_CMD=$(printf "bash %q --autostart; exec bash" "$DEMO_DIR/syncloop.sh")

# Pane order: top row alice | bob | carol, bottom row dan | eve | sync loop.
run tmux new-session -d -s "$TMUX_SESSION" -x 240 -y 66 -c "$DEMO_DIR" "$(cmd_for alice)"
run tmux set-option -t "$TMUX_SESSION" pane-border-status top
run tmux set-option -t "$TMUX_SESSION" pane-border-format ' #{pane_title} '
run tmux set-option -t "$TMUX_SESSION" status off
run tmux split-window -v -t "$TMUX_SESSION:0.0" -c "$DEMO_DIR" "$(cmd_for dan)"
run tmux split-window -h -t "$TMUX_SESSION:0.0" -c "$DEMO_DIR" "$(cmd_for bob)"
run tmux split-window -h -t "$TMUX_SESSION:0.1" -c "$DEMO_DIR" "$(cmd_for carol)"
run tmux split-window -h -t "$TMUX_SESSION:0.3" -c "$DEMO_DIR" "$(cmd_for eve)"
run tmux split-window -h -t "$TMUX_SESSION:0.4" -c "$DEMO_DIR" "$LOOP_CMD"
# Even 3 columns in each row: tiled is 3 rows x 2 cols for 6 panes, so
# resize explicitly to a third of the width.
i=0
for t in "alice: waitlist" "bob: customer import" "carol: image uploads" "dan: webhook" "eve: staging deploy" "muretto sync"; do
	run tmux select-pane -t "$TMUX_SESSION:0.$i" -T "$t"
	i=$((i + 1))
done
for p in 0 1 3 4; do
	run tmux resize-pane -t "$TMUX_SESSION:0.$p" -x 80
done

say ""
say "${C_B}Dashboard:${C_0} $WEB_URL/login"
say "  (press the development-only sign-in button: it signs you in as alice with a dev token from the local server)"
say "${C_B}Start:${C_0} press Enter in the bottom-right pane to start all five sessions at once."
say "${C_B}Stop and clean up:${C_0} demo/reset.sh --apply"
if [ "$NO_ATTACH" != 1 ] && [ "$DRY_RUN" != 1 ]; then
	exec tmux attach -t "$TMUX_SESSION"
fi
[ "$DRY_RUN" != 1 ] || {
	say ""
	step "Each pane runs (session.sh --dry-run):"
	for d in $ONLY; do DRY_RUN=1 "$DEMO_DIR/session.sh" --dry-run "$d"; done
}
