#!/usr/bin/env bash
# examples/live-demo/setup.sh: one-time preparation of the TEST accounts.
#
#   examples/live-demo/setup.sh            dry run: print what would be done (no network)
#   examples/live-demo/setup.sh --apply    do it (asks you to type the test project ref)
#
# Demo tool for TEST accounts only. Muretto itself never writes anything;
# this script is not part of Muretto.
set -euo pipefail
# shellcheck source=examples/live-demo/lib.sh
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

DRY_RUN=1
while [ $# -gt 0 ]; do
	case $1 in
	--apply) DRY_RUN=0 ;;
	--dry-run) DRY_RUN=1 ;;
	-h | --help) sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//' && exit 0 ;;
	*) say "usage: examples/live-demo/setup.sh [--apply]" && exit 2 ;;
	esac
	shift
done
export DRY_RUN

load_env
require_demo
check_tools curl jq
[ "$DRY_RUN" = 0 ] || say "${C_YEL}Dry run.${C_0} Nothing is sent. Use --apply to run it."

step "Checking that the Supabase projects are test projects"
assert_test_projects
confirm_ref "setup.sh changes default privileges on the test project(s)."

# =============================================================================
# 1. Supabase: re-grant default privileges on the TEST project(s)
# =============================================================================
#
# WHY THIS EXISTS (read before you run it)
#
# The demo's wow moment is bob's agent creating public.customers with SQL.
# Postgres leaves row level security OFF for tables created with SQL, and
# that is unchanged. What changed is the Data API grants:
#
#   - Supabase announced (2026-04-28) that new projects from May 30, 2026, and
#     ALL projects from October 30, 2026, stop granting new public tables to
#     the anon and authenticated roles. Without a grant, PostgREST answers
#     42501 "permission denied", prove.sh gets no rows, and Muretto correctly
#     does NOT flag the table (it is not reachable).
#   - Every project created before that change, which is most production
#     projects today, still has these default privileges. On those, an
#     RLS-off table in public is readable by anyone holding the public key.
#
# So the demo restores the pre-change default ON THE TEST PROJECT ONLY, once,
# with the statement Supabase's own docs show (adapters/supabase/research.md Q7,
# option (a) in docs/OPEN_QUESTIONS.md §5). examples/live-demo/SCRIPT.md discloses this in
# the video's on-screen text. Never run this on a real project: it makes every
# future table in public reachable through the Data API.
#
# To undo it on the test project:
#   alter default privileges for role postgres in schema public
#     revoke select, insert, update, delete on tables from anon, authenticated;
# =============================================================================
GRANT_SQL="alter default privileges for role postgres in schema public grant select, insert, update, delete on tables to anon, authenticated, service_role;"
CHECK_SQL="select pg_get_userbyid(defaclrole) as owner, defaclnamespace::regnamespace as schema, defaclobjtype as type, defaclacl::text as acl from pg_default_acl where defaclnamespace = 'public'::regnamespace and defaclobjtype = 'r';"

for ref in $(project_refs); do
	step "Supabase $ref: re-grant default privileges in public to anon, authenticated (test project only)"
	sql "$ref" "$GRANT_SQL" >/dev/null
	step "Supabase $ref: check the default privileges"
	if [ "$DRY_RUN" = 1 ]; then
		sql "$ref" "$CHECK_SQL"
	else
		out=$(sql "$ref" "$CHECK_SQL")
		printf '%s\n' "$out" | jq -r '.[]? | "  \(.owner) \(.schema) \(.acl)"'
		printf '%s' "$out" | grep -q 'anon=' || die "anon is still missing from the default privileges of $ref"
		say "  ${C_GRN}anon has default table privileges on $ref ✓${C_0}"
	fi
done

# =============================================================================
# 2. Vercel: the TEST team's default deployment protection (by hand)
# =============================================================================
step "Vercel team $VERCEL_TEAM_SLUG: deployment protection (manual, one time)"
cat >&2 <<EOF
  New Vercel projects default to Standard Protection: previews need a Vercel
  login, so a demo preview would not be public (docs/OPEN_QUESTIONS.md §5b).
  For the demo, on the TEST team only:

    1. Open https://vercel.com/$VERCEL_TEAM_SLUG/~/settings
    2. Find Deployment Protection (team defaults for new projects).
    3. Set Vercel Authentication / Standard Protection to Disabled ("None")
       for new projects, and save.

  This does not change existing projects. examples/live-demo/SCRIPT.md states it on screen.
  If your plan has no team-level default, alice's and eve's previews stay
  protected: Muretto then shows them as protected, which is also true.
  Plan notes: Hobby allows 1 concurrent build (alice's and eve's deploys
  queue) and is for non-commercial use; a Pro trial avoids both.
EOF

# =============================================================================
# 3. Local checks (no network)
# =============================================================================
step "Local prerequisites"
DRY_RUN=1 check_tools claude tmux jq curl go npm uuidgen vercel muretto
[ -d "$REPO_ROOT/apps/dashboard/node_modules" ] || warn "apps/dashboard/node_modules is missing: (cd apps/dashboard && npm ci)"
say ""
say "Next: examples/live-demo/run.sh --dry-run, then examples/live-demo/run.sh"
