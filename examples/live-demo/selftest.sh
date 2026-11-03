#!/usr/bin/env bash
# examples/live-demo/selftest.sh: offline check of the demo's local plumbing.
#
# No provider accounts, no tokens, no network beyond 127.0.0.1, no Claude
# sessions. It uses the same lib.sh functions run.sh uses:
#   1. start the sync server with dev login on 127.0.0.1 (in-memory)
#   2. give alice and bob their own MURETTO_HOME, a dev token, sync on
#   3. alice creates the team, bob joins with an invite
#   4. feed bob's hooks the events of a fake session (as Claude Code would)
#   5. `muretto sync now` as bob
#   6. check the server has bob's session, in his view and in alice's team view
# Then it stops the server and deletes its scratch directory.
set -euo pipefail
# shellcheck source=examples/live-demo/lib.sh
. "$(cd "$(dirname "$0")" && pwd)/lib.sh"

DRY_RUN=0
SCRATCH=$(mktemp -d "${TMPDIR:-/tmp}/muretto-selftest.XXXXXX")
DEMO_RUN_DIR=$SCRATCH/demo-run
DEMO_SERVER_PORT=${SELFTEST_PORT:-18787}
DEMO_WEB_PORT=3999
SERVER_URL=http://127.0.0.1:$DEMO_SERVER_PORT
WEB_URL=http://localhost:$DEMO_WEB_PORT
DEMO_ORG_NAME="Acme (selftest)"
# Never let a provider token from the caller's shell reach muretto here.
unset MURETTO_SUPABASE_TOKEN MURETTO_VERCEL_TOKEN SUPABASE_ACCESS_TOKEN VERCEL_TOKEN MURETTO_SYNC_TOKEN
clean_claude_env
# Never read the real ~/.claude here.
CLAUDE_SRC_DIR=$SCRATCH/presenter-claude

cleanup() {
	stop_pidfile "$DEMO_RUN_DIR/server.pid" >/dev/null 2>&1 || true
	rm -rf "$SCRATCH"
}
trap cleanup EXIT

ok() { printf '%sok%s   %s\n' "$C_GRN" "$C_0" "$*"; }
fail() { die "FAIL: $*"; }

check_tools go jq curl muretto
mkdir -p "$DEMO_RUN_DIR"

start_server
ok "server healthy at $SERVER_URL with MURETTO_DEV_LOGIN=1"

for d in alice bob; do
	setup_dev "$d" 2>"$DEMO_RUN_DIR/setup-$d.log" || {
		cat "$DEMO_RUN_DIR/setup-$d.log" >&2
		fail "setup_dev $d"
	}
	[ -s "$(dev_token_file "$d")" ] || fail "no dev token for $d"
	jq -e --arg s "$SERVER_URL" '.sync.enabled == true and .sync.server == $s' "$(dev_home "$d")/config.json" >/dev/null ||
		fail "sync not on for $d"
	ok "$d: own MURETTO_HOME, dev token ($(cut -c1-4 "$(dev_token_file "$d")")…), sync on → $SERVER_URL"
done
muretto_as bob sync status | sed 's/^/      /'

# A stand-in for the presenter's ~/.claude: bob's own project dir holds one
# transcript (the research fixture); another project holds a decoy that bob's
# muretto must never see.
FIXTURE=$REPO_ROOT/internal/hook/testdata/claude-code/transcript-sample.jsonl
FIXTURE_SID=0b6e3c1a-7f2d-4c55-9a10-3e8d2f4b6a01
DECOY_SID=11111111-1111-4111-8111-111111111111
bob_proj=$CLAUDE_SRC_DIR/projects/$(claude_project_name "$(cd "$(dev_workdir bob)" && pwd -P)")
mkdir -p "$bob_proj" "$CLAUDE_SRC_DIR/projects/-home-presenter-other-project"
cp "$FIXTURE" "$bob_proj/$FIXTURE_SID.jsonl"
sed "s/$FIXTURE_SID/$DECOY_SID/g" "$FIXTURE" >"$CLAUDE_SRC_DIR/projects/-home-presenter-other-project/$DECOY_SID.jsonl"

setup_org alice bob
ok "team '$DEMO_ORG_NAME' created by alice; bob joined by invite"

hooks_settings | jq -e '.hooks.PostToolUse[0].hooks[0].args == ["hook","PostToolUse"] and .enabledMcpjsonServers == ["supabase"]' >/dev/null ||
	fail "hooks settings"
mcp_config abcdefghijklmnopqrst | jq -e '.mcpServers.supabase.headers.Authorization == "Bearer ${SUPABASE_ACCESS_TOKEN}"' >/dev/null ||
	fail "mcp config"
ok "per-run hooks settings and MCP config are valid JSON"

# A fake session for bob, delivered exactly as Claude Code would: one JSON
# object on the hook's stdin per event.
SID=$(uuidgen | tr '[:upper:]' '[:lower:]')
WORK=$(dev_workdir bob)
mkdir -p "$WORK"
# As run.sh does: the Supabase MCP server pinned in the workdir's .mcp.json.
mcp_config abcdefghijklmnopqrst >"$WORK/.mcp.json"
TP=$SCRATCH/transcripts/$SID.jsonl
hook() {
	MURETTO_HOME=$(dev_home bob) "$MURETTO_BIN" hook "$1"
}
jq -cn --arg s "$SID" --arg t "$TP" --arg c "$WORK" \
	'{session_id:$s, transcript_path:$t, cwd:$c, hook_event_name:"SessionStart", source:"startup", session_title:"bob: Prototype a customer import"}' | hook SessionStart
jq -cn --arg s "$SID" --arg t "$TP" --arg c "$WORK" \
	'{session_id:$s, transcript_path:$t, cwd:$c, hook_event_name:"UserPromptSubmit", prompt_id:"5d1f7e2a-0c3b-4e8f-b1a2-9c7d6e5f4a30", permission_mode:"dontAsk", prompt:"Prototype a customer import. Run this SQL exactly as written"}' | hook UserPromptSubmit
Q="create table public.customers (id bigint generated always as identity primary key, name text not null, email text not null); insert into public.customers (name, email) values ('Ada Example', 'ada@example.com');"
jq -cn --arg s "$SID" --arg t "$TP" --arg c "$WORK" --arg q "$Q" '{
	session_id:$s, transcript_path:$t, cwd:$c, hook_event_name:"PostToolUse",
	prompt_id:"5d1f7e2a-0c3b-4e8f-b1a2-9c7d6e5f4a30", permission_mode:"dontAsk",
	tool_name:"mcp__supabase__execute_sql",
	tool_input:{query:$q},
	tool_response:[{type:"text", text:"{\"result\":\"[]\"}"}],
	tool_use_id:"toolu_01FAKEselftest00000000000", duration_ms:5,
	mcp_server:{name:"supabase", source:"project"}}' | hook PostToolUse
jq -cn --arg s "$SID" --arg t "$TP" --arg c "$WORK" \
	'{session_id:$s, transcript_path:$t, cwd:$c, hook_event_name:"SessionEnd", reason:"other"}' | hook SessionEnd
n=$(find "$(dev_home bob)/spool" -name '*.json' | wc -l | tr -d ' ')
[ "$n" -ge 4 ] || fail "expected 4 spool files, found $n"
ok "4 hook events spooled for bob ($n files in $(dev_home bob)/spool)"
grep -l '"mcp_params":{"project_ref":"abcdefghijklmnopqrst"}' "$(dev_home bob)"/spool/*.json >/dev/null ||
	fail "the hook did not record the pinned project_ref from .mcp.json"
if grep -qE 'SUPABASE_ACCESS_TOKEN|mcp[.]supabase[.]com' "$(dev_home bob)"/spool/*.json; then
	fail "the spool holds MCP config beyond the project_ref"
fi
ok "the hook recorded the project_ref pinned in .mcp.json, and nothing else from it"

out=$(muretto_as bob sync now 2>&1) || fail "muretto sync now: $out"
printf '%s' "$out" | grep -q 'Synced.' || fail "sync now said: $out"
ok "muretto sync now (bob): $(printf '%s' "$out" | tail -n 1)"

me=$(curl -fsS -H "Authorization: Bearer $(cat "$(dev_token_file bob)")" "$SERVER_URL/v1/answer?scope=me")
printf '%s' "$me" | jq -e --arg s "$SID" '[.sessions[] | select(.id == $s or .session_id == $s or (tostring | contains($s)))] | length >= 1' >/dev/null ||
	fail "bob's session $SID is not on the server: $(printf '%s' "$me" | jq -c '.summary')"
printf '%s' "$me" | jq -e --arg f "$FIXTURE_SID" --arg d "$DECOY_SID" '
	[.sessions[].session.id] | (index($f) != null) and (index($d) == null) and length == 2' >/dev/null ||
	fail "bob should see his hook session and his own transcript only: $(printf '%s' "$me" | jq -c '[.sessions[].session.id]')"
ok "bob's muretto read his own transcript through the symlinked CLAUDE_CONFIG_DIR, not the decoy"
ok "server has bob's session: $(printf '%s' "$me" | jq -c '{summary: .summary | {sessions, agent_created}, task_title: [.sessions[].session.task_title], sources: .sessions[0].session.sources}')"

# alice reads the team view as the dashboard would: Bearer auth (her dev
# token stands in for a Supabase sign-in because the server runs dev login).
org=$(cat "$DEMO_RUN_DIR/org-id" 2>/dev/null || true)
[ -n "$org" ] || org=$(api_call alice GET /v1/me | jq -r '.orgs[0].id')
[ -n "$org" ] && [ "$org" != null ] || fail "alice has no team"
team=$(api_call alice GET "/v1/orgs/$org/answer") || fail "alice could not read the team view"
printf '%s' "$team" | jq -e --arg s "$SID" '[.sessions[] | tostring | select(contains($s))] | length >= 1' >/dev/null ||
	fail "bob's session is missing from alice's team view"
ok "alice's team view shows bob's session ($(printf '%s' "$team" | jq -r '[.sessions[].developer_name // empty] | unique | join(", ")'))"

say ""
say "${C_GRN}${C_B}selftest passed${C_0} (no provider calls; scratch dir removed on exit)"
