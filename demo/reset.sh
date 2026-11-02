#!/usr/bin/env bash
# demo/reset.sh: remove everything the demo created, so it can run again.
#
#   THIS IS A DEMO TOOL FOR TEST ACCOUNTS ONLY. IT IS NOT PART OF MURETTO.
#   MURETTO ITSELF NEVER CREATES, CHANGES OR DELETES ANYTHING IN ANY ACCOUNT.
#
#   demo/reset.sh                  dry run: print every command (no network)
#   demo/reset.sh --apply          list what the demo created, show it, ask you
#                                  to type the test project ref, then delete it
#   demo/reset.sh --local --apply  only the local part (no provider calls)
#
# What it can delete, and nothing else:
#   - Supabase tables in the explicit list DEMO_TABLES (lib.sh): customers,
#     waitlist; only in the schema public of the configured test project(s).
#   - Supabase storage buckets and edge functions whose id/slug starts with
#     "demo-".
#   - Vercel projects in the test team whose name starts with "demo-", and
#     every deployment of those projects.
#   - Locally: the tmux session, the sync server (its in-memory data goes with
#     it), the dashboard dev server, and demo/.run (the five MURETTO_HOMEs,
#     dev tokens, logs).
# Endpoints: adapters/supabase/research.md Q9 and adapters/vercel/research.md Q9.
set -euo pipefail
# shellcheck source=demo/lib.sh
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

DRY_RUN=1
LOCAL_ONLY=0
while [ $# -gt 0 ]; do
	case $1 in
	--apply) DRY_RUN=0 ;;
	--dry-run) DRY_RUN=1 ;;
	--local) LOCAL_ONLY=1 ;;
	-h | --help) sed -n '2,23p' "$0" | sed 's/^# \{0,1\}//' && exit 0 ;;
	*) say "usage: demo/reset.sh [--apply] [--local]" && exit 2 ;;
	esac
	shift
done
export DRY_RUN

load_env
require_demo
check_tools curl jq
[ "$DRY_RUN" = 0 ] || say "${C_YEL}Dry run.${C_0} Nothing is sent or deleted. Use --apply to run it."

# ---------------------------------------------------------------- local
# demo_transcript_dirs prints the Claude Code project directories holding the
# demo sessions' transcripts: one per developer workdir, in its logical and
# physical spelling. Every run reuses the same workdirs, so without removing
# them a fresh Muretto reads earlier runs' sessions as competing evidence
# (seen live 2026-10-27: "2 sessions claim to have created this").
demo_transcript_dirs() {
	local d work phys
	for d in $DEMO_DEVS; do
		work=$(dev_workdir "$d")
		printf '%s/projects/%s\n' "$CLAUDE_SRC_DIR" "$(claude_project_name "$work")"
		phys=$( (cd "$work" 2>/dev/null && pwd -P) || true)
		if [ -n "$phys" ] && [ "$phys" != "$work" ]; then
			printf '%s/projects/%s\n' "$CLAUDE_SRC_DIR" "$(claude_project_name "$phys")"
		fi
	done
}

reset_local() {
	step "Local: tmux session, sync server, dashboard, demo/.run, demo transcripts"
	local t
	if [ "$DRY_RUN" = 1 ]; then
		show_cmd tmux kill-session -t "$TMUX_SESSION"
		show_cmd kill "\$(cat $DEMO_RUN_DIR/server.pid)"
		show_cmd kill "\$(cat $DEMO_RUN_DIR/web.pid)" "# and its child processes"
		demo_transcript_dirs | while IFS= read -r t; do show_cmd rm -rf "$t"; done
		show_cmd rm -rf "$DEMO_RUN_DIR"
		return 0
	fi
	if command -v tmux >/dev/null 2>&1 && tmux has-session -t "$TMUX_SESSION" 2>/dev/null; then
		tmux kill-session -t "$TMUX_SESSION"
	fi
	stop_pidfile "$DEMO_RUN_DIR/web.pid"
	stop_pidfile "$DEMO_RUN_DIR/server.pid"
	# Only the demo workdirs' own transcript directories, never anything else.
	demo_transcript_dirs | while IFS= read -r t; do
		case $t in "$CLAUDE_SRC_DIR"/projects/*-work-*) [ ! -d "$t" ] || rm -rf "$t" ;; esac
	done
	# Only ever remove the demo's own run directory.
	case $DEMO_RUN_DIR in */.run | */demo-run*) rm -rf "$DEMO_RUN_DIR" ;; *) die "refusing to delete unexpected run dir $DEMO_RUN_DIR" ;; esac
	say "  local state cleared"
}

if [ "$LOCAL_ONLY" = 1 ]; then
	reset_local
	exit 0
fi

step "Checking that the Supabase projects are test projects"
assert_test_projects

# ---------------------------------------------------------------- plan
# Collect what exists (reads only), then confirm, then delete.
PLAN=$(mktemp)
trap 'rm -f "$PLAN"' EXIT

secret_for() {
	if [ "$1" = "$SUPABASE_PROJECT_REF" ]; then printf '%s' SUPABASE_SECRET_KEY; else printf '%s' SUPABASE_SECRET_KEY_2; fi
}

for ref in $(project_refs); do
	for t in $DEMO_TABLES; do
		printf 'table\t%s\t%s\n' "$ref" "$t" >>"$PLAN"
	done
	step "Supabase $ref: list edge functions and buckets"
	if [ "$DRY_RUN" = 1 ]; then
		api GET "$SUPABASE_API/v1/projects/$ref/functions" SUPABASE_ACCESS_TOKEN
		api GET "$SUPABASE_API/v1/projects/$ref/storage/buckets" SUPABASE_ACCESS_TOKEN
		printf 'function\t%s\t%s\n' "$ref" "${DEMO_PREFIX}webhook" >>"$PLAN"
		printf 'bucket\t%s\t%s\n' "$ref" "${DEMO_PREFIX}uploads" >>"$PLAN"
		continue
	fi
	for slug in $(api GET "$SUPABASE_API/v1/projects/$ref/functions" SUPABASE_ACCESS_TOKEN | jq -r '.[]?.slug'); do
		if is_demo_name "$slug"; then printf 'function\t%s\t%s\n' "$ref" "$slug" >>"$PLAN"; fi
	done
	for id in $(api GET "$SUPABASE_API/v1/projects/$ref/storage/buckets" SUPABASE_ACCESS_TOKEN | jq -r '.[]?.id'); do
		if is_demo_name "$id"; then printf 'bucket\t%s\t%s\n' "$ref" "$id" >>"$PLAN"; fi
	done
done

step "Vercel $VERCEL_TEAM_SLUG: list demo- projects and their deployments"
if [ "$DRY_RUN" = 1 ]; then
	api GET "$VERCEL_API/v10/projects?teamId=$VERCEL_TEAM_ID&search=$DEMO_PREFIX&limit=100" VERCEL_TOKEN
	api GET "$VERCEL_API/v7/deployments?teamId=$VERCEL_TEAM_ID&projectId=prj_example&limit=100" VERCEL_TOKEN
	printf 'deployment\t%s\t%s\n' prj_example dpl_example >>"$PLAN"
	printf 'project\t%s\t%s\n' prj_example "${DEMO_PREFIX}waitlist" >>"$PLAN"
else
	projects=$(api GET "$VERCEL_API/v10/projects?teamId=$VERCEL_TEAM_ID&search=$DEMO_PREFIX&limit=100" VERCEL_TOKEN)
	printf '%s' "$projects" | jq -r '.projects[]? | "\(.id)\t\(.name)"' | while IFS="$(printf '\t')" read -r pid pname; do
		is_demo_name "$pname" || continue
		# Deployments first (the spec does not say a project delete cascades).
		until_ts=''
		while :; do
			q="teamId=$VERCEL_TEAM_ID&projectId=$pid&limit=100"
			[ -z "$until_ts" ] || q="$q&until=$until_ts"
			page=$(api GET "$VERCEL_API/v7/deployments?$q" VERCEL_TOKEN)
			printf '%s' "$page" | jq -r --arg p "$pid" '.deployments[]? | "deployment\t\($p)\t\(.uid)"' >>"$PLAN"
			until_ts=$(printf '%s' "$page" | jq -r '.pagination.next // empty')
			[ -n "$until_ts" ] || break
		done
		printf 'project\t%s\t%s\n' "$pid" "$pname" >>"$PLAN"
	done
fi

step "Plan"
awk -F '\t' '{ printf "  delete %-10s %-28s (%s)\n", $1, $3, $2 }' "$PLAN" >&2
[ -s "$PLAN" ] || say "  nothing to delete"
confirm_ref "Delete the items above from the TEST accounts? (local state is cleared too)"

# ---------------------------------------------------------------- delete
DROP=''
for t in $DEMO_TABLES; do
	printf '%s' "$t" | grep -Eq '^[a-z_][a-z0-9_]*$' || die "bad table name $t"
	DROP="$DROP${DROP:+, }public.$t"
done

# Storage policies that carol's session adds on storage.objects for the
# demo bucket. Their names are the agent's choice, so match on the bucket id;
# a leftover policy makes the next run's CREATE POLICY fail on camera.
DROP_POLICIES="do \$\$ declare p record; begin
  for p in select policyname from pg_policies
    where schemaname = 'storage' and tablename = 'objects'
      and (coalesce(qual, '') || coalesce(with_check, '')) like '%${DEMO_PREFIX}uploads%'
  loop execute format('drop policy %I on storage.objects', p.policyname); end loop;
end \$\$;"

for ref in $(project_refs); do
	step "Supabase $ref: drop $DROP"
	sql "$ref" "drop table if exists $DROP cascade;" >/dev/null
	step "Supabase $ref: drop storage.objects policies on the ${DEMO_PREFIX}uploads bucket"
	sql "$ref" "$DROP_POLICIES" >/dev/null || FAIL_POLICIES=1
done

FAIL=${FAIL_POLICIES:-0}
while IFS="$(printf '\t')" read -r kind scope name; do
	case $kind in
	table) ;;
	function)
		is_demo_name "$name" || die "refusing to delete function $name (no $DEMO_PREFIX prefix)"
		step "Supabase $scope: delete edge function $name"
		api DELETE "$SUPABASE_API/v1/projects/$scope/functions/$name" SUPABASE_ACCESS_TOKEN >/dev/null || FAIL=1
		;;
	bucket)
		is_demo_name "$name" || die "refusing to delete bucket $name (no $DEMO_PREFIX prefix)"
		keyvar=$(secret_for "$scope")
		if [ -z "$(eval "printf '%s' \"\${$keyvar:-}\"")" ] && [ "$DRY_RUN" != 1 ]; then
			warn "no $keyvar: delete bucket $name in the dashboard (Storage) of project $scope"
			FAIL=1
			continue
		fi
		# The Management API has no bucket delete; the Storage API does
		# (empty, then delete). Paths from supabase storage-js; see README.
		step "Supabase $scope: empty and delete bucket $name"
		base="https://$scope.supabase.co/storage/v1/bucket/$name"
		if [ "$DRY_RUN" = 1 ]; then
			show_cmd curl -sS -X POST "$base/empty" -H "apikey: \${$keyvar}" -H "Authorization: Bearer \${$keyvar}"
			show_cmd curl -sS -X DELETE "$base" -H "apikey: \${$keyvar}" -H "Authorization: Bearer \${$keyvar}"
		else
			key=$(eval "printf '%s' \"\${$keyvar}\"")
			curl -fsS -X POST "$base/empty" -H "apikey: $key" -H "Authorization: Bearer $key" >/dev/null || FAIL=1
			curl -fsS -X DELETE "$base" -H "apikey: $key" -H "Authorization: Bearer $key" >/dev/null || FAIL=1
		fi
		;;
	deployment)
		printf '%s' "$name" | grep -Eq '^dpl_[A-Za-z0-9]+$' || die "unexpected deployment id $name"
		step "Vercel: delete deployment $name (project $scope)"
		api DELETE "$VERCEL_API/v13/deployments/$name?teamId=$VERCEL_TEAM_ID" VERCEL_TOKEN >/dev/null || FAIL=1
		;;
	project)
		is_demo_name "$name" || die "refusing to delete project $name (no $DEMO_PREFIX prefix)"
		step "Vercel: delete project $name"
		api DELETE "$VERCEL_API/v9/projects/$scope?teamId=$VERCEL_TEAM_ID" VERCEL_TOKEN >/dev/null || FAIL=1
		;;
	esac
done <"$PLAN"

reset_local
if [ "$FAIL" = 1 ]; then
	die "some deletes failed (see above); run demo/reset.sh --apply again or finish by hand"
fi
[ "$DRY_RUN" = 1 ] || say "${C_GRN}Reset done.${C_0} The test accounts are back to their pre-demo state."
