#!/usr/bin/env bash
# demo/syncloop.sh: the sixth pane of the demo grid.
#
# Waits for Enter (or --autostart), writes the "go" file that releases the
# five sessions at once, then every DEMO_SYNC_INTERVAL seconds runs
# `muretto sync now` for each developer and prints one status line. Every
# DEMO_REFRESH_INTERVAL seconds it also forces a fresh provider check for bob
# (Muretto caches scans for 60 s), so a fixed table turns green quickly.
#
# Muretto only reads the providers here; nothing is written to any account.
set -uo pipefail
# shellcheck source=demo/lib.sh
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

AUTOSTART=0
ONCE=0
while [ $# -gt 0 ]; do
	case $1 in
	--autostart) AUTOSTART=1 ;;
	--once) ONCE=1 AUTOSTART=1 ;;
	--dry-run) DRY_RUN=1 ;;
	*) say "usage: demo/syncloop.sh [--autostart] [--once] [--dry-run]" && exit 2 ;;
	esac
	shift
done

load_env
require_demo
clean_claude_env

if [ "$DRY_RUN" = 1 ]; then
	run touch "$DEMO_RUN_DIR/go"
	for d in $DEMO_DEVS; do muretto_as "$d" sync now; done
	muretto_as bob exposed --refresh
	for d in $DEMO_DEVS; do
		show_cmd curl -fsS -H "Authorization: Bearer \$(cat $(dev_token_file "$d"))" "$SERVER_URL/v1/answer?scope=me"
	done
	exit 0
fi

printf '\033]2;%s\007' "muretto sync"
if [ "$AUTOSTART" != 1 ]; then
	printf '%sMuretto%s sync loop. Press Enter to start all five sessions.' "$C_B" "$C_0"
	IFS= read -r _ || true
fi
touch "$DEMO_RUN_DIR/go"
printf '\n%sstarted%s %s\n\n' "$C_GRN" "$C_0" "$(date +%H:%M:%S)"

running() {
	local n=0 d
	for d in $DEMO_DEVS; do
		[ -s "$DEMO_RUN_DIR/logs/$d.jsonl" ] || continue
		grep -q '"type":"result"' "$DEMO_RUN_DIR/logs/$d.jsonl" || n=$((n + 1))
	done
	printf '%s' "$n"
}

# summary_of <dev>: "agent_created exposed" from the local server's copy of
# that developer's synced data (no provider calls).
summary_of() {
	curl -fsS -m 3 -H "Authorization: Bearer $(cat "$(dev_token_file "$1")")" "$SERVER_URL/v1/answer?scope=me" 2>/dev/null |
		jq -r '"\(.summary.agent_created) \(.summary.exposed)"' 2>/dev/null || echo "0 0"
}

last_refresh=0
while :; do
	marks=''
	created=0
	exposed=0
	now=$(date +%s)
	if [ $((now - last_refresh)) -ge "$DEMO_REFRESH_INTERVAL" ]; then
		muretto_as bob exposed --refresh >/dev/null 2>&1 || true
		last_refresh=$now
	fi
	for d in $DEMO_DEVS; do
		if muretto_as "$d" sync now >/dev/null 2>>"$DEMO_RUN_DIR/logs/sync-$d.stderr"; then
			marks="$marks $d ${C_GRN}✓${C_0}"
		else
			marks="$marks $d ${C_RED}✗${C_0}"
		fi
		read -r c e <<EOF
$(summary_of "$d")
EOF
		created=$((created + ${c:-0}))
		# Every developer sees every resource, so take the max, not the sum.
		[ "${e:-0}" -le "$exposed" ] || exposed=${e:-0}
	done
	ex="$exposed exposed"
	[ "$exposed" -eq 0 ] || ex="${C_RED}${C_B}$exposed exposed${C_0}"
	printf '%s%s%s %s · %s created · %s · %s running\n' "$C_DIM" "$(date +%H:%M:%S)" "$C_0" "$marks" "$created" "$ex" "$(running)"
	[ "$ONCE" = 1 ] && exit 0
	sleep "$DEMO_SYNC_INTERVAL"
done
