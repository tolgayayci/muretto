#!/usr/bin/env bash
# examples/live-demo/prove.sh: the proof moment.
#
# 1. curl the primary test project's public REST endpoint for bob's table,
#    sending ONLY the publishable (anon) key, the key every browser has.
#    Rows coming back means anyone on the internet can read them.
# 2. Show what Muretto says about it, as bob: `muretto why customers` (the
#    evidence: which session and which SQL created it) and
#    `muretto fix customers` (the exact SQL a human can run; Muretto never
#    runs it).
#
#   examples/live-demo/prove.sh            both parts
#   examples/live-demo/prove.sh --curl     only the curl (e.g. again after the fix)
#   examples/live-demo/prove.sh --check    one line; exit 0 only if rows come back AND
#                            Muretto (as bob) flags customers as exposed
#                            (for repeated reliability runs)
#   examples/live-demo/prove.sh --dry-run  print the commands
#
# Reading with a public key is what the attacker would do; it writes nothing.
set -euo pipefail
# shellcheck source=examples/live-demo/lib.sh
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

MODE=all
while [ $# -gt 0 ]; do
	case $1 in
	--curl) MODE=curl ;;
	--check) MODE=check ;;
	--dry-run) DRY_RUN=1 ;;
	-h | --help) sed -n '2,19p' "$0" | sed 's/^# \{0,1\}//' && exit 0 ;;
	*) say "usage: examples/live-demo/prove.sh [--curl|--check] [--dry-run]" && exit 2 ;;
	esac
	shift
done

load_env
require_demo
check_tools curl jq
[ "$MODE" = check ] || [ "$MODE" = curl ] || check_tools muretto

URL="https://$SUPABASE_PROJECT_REF.supabase.co/rest/v1/customers?select=*"

if [ "$DRY_RUN" = 1 ]; then
	show_cmd curl -sS "$URL" -H "apikey: $SUPABASE_PUBLISHABLE_KEY" -H "Authorization: Bearer $SUPABASE_PUBLISHABLE_KEY"
	if [ "$MODE" = all ]; then
		muretto_as bob why customers
		muretto_as bob fix customers
	fi
	exit 0
fi

body=$(mktemp)
trap 'rm -f "$body"' EXIT
code=$(curl -sS -o "$body" -w '%{http_code}' "$URL" \
	-H "apikey: $SUPABASE_PUBLISHABLE_KEY" -H "Authorization: Bearer $SUPABASE_PUBLISHABLE_KEY") || code=000
rows=$(jq -r 'if type == "array" then length else -1 end' "$body" 2>/dev/null || echo -1)

if [ "$MODE" = check ]; then
	# One line for the reliability log: are rows public, and does Muretto
	# (as bob, fresh check) flag public.customers as exposed?
	flagged=no
	if [ -x "$MURETTO_BIN" ] && [ -d "$(dev_home bob)" ]; then
		if muretto_as bob exposed --refresh --json 2>/dev/null | jq -e '
			[.resources[]? | select(.name == "customers" or .name == "public.customers") | .id] as $ids
			| [.findings[]? | select(.state == "exposed") | select(.resource_id as $r | $ids | index($r))]
			| length > 0' >/dev/null; then
			flagged=yes
		fi
	fi
	printf 'http=%s rows=%s muretto_flagged=%s\n' "$code" "$rows" "$flagged"
	[ "$code" = 200 ] && [ "$rows" -gt 0 ] && [ "$flagged" = yes ]
	exit
fi

# On screen: the key is shortened. It is public by design, but there is no
# reason to put a full key in a video.
short=$(printf '%s' "$SUPABASE_PUBLISHABLE_KEY" | cut -c1-18)
printf '%s$%s curl %s \\\n    -H "apikey: %s…"\n\n' "$C_B" "$C_0" "'$URL'" "$short"
if [ "$code" = 200 ] && [ "$rows" -gt 0 ]; then
	jq -C . "$body"
	printf '\n%s%s rows readable by anyone with the public key.%s\n' "$C_RED$C_B" "$rows" "$C_0"
elif [ "$code" = 200 ]; then
	jq -C . "$body"
	printf '\n%sNo rows: the public key reads nothing.%s\n' "$C_GRN$C_B" "$C_0"
else
	jq -C . "$body" 2>/dev/null || cat "$body"
	printf '\n%sHTTP %s%s\n' "$C_YEL" "$code" "$C_0"
	if grep -q 42501 "$body" 2>/dev/null; then
		say "42501: the table has no grant for anon. Was examples/live-demo/setup.sh --apply run on this project? (see README.md)"
	fi
fi

[ "$MODE" = all ] || exit 0
printf '\n%s$%s muretto why customers\n' "$C_B" "$C_0"
muretto_as bob why customers
printf '\n%s$%s muretto fix customers\n' "$C_B" "$C_0"
muretto_as bob fix customers
