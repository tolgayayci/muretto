# shellcheck shell=bash
# shellcheck disable=SC2034 # variables here are used by the scripts that source this file
# demo/lib.sh: shared guards and helpers for the Muretto demo harness.
#
# Sourced by run.sh, setup.sh, reset.sh, prove.sh, session.sh, syncloop.sh and
# selftest.sh. Written for bash 3.2 (the macOS default): no associative
# arrays, no mapfile, no ${var,,}.
#
# This harness is a demo tool for TEST accounts only. It is not part of
# Muretto. Muretto itself never creates, changes or deletes anything.

DEMO_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$DEMO_DIR/.." && pwd)
DEMO_ENV_FILE=${DEMO_ENV_FILE:-$DEMO_DIR/.env}
DEMO_RUN_DIR=${DEMO_RUN_DIR:-$DEMO_DIR/.run}
MURETTO_BIN=${MURETTO_BIN:-$REPO_ROOT/bin/muretto}
DRY_RUN=${DRY_RUN:-0}

# The five developers, their display names and their tasks (BRIEF §13).
DEMO_DEVS="alice bob carol dan eve"
TMUX_SESSION=muretto-demo

# Everything the demo may create. reset.sh deletes only these: tables from
# this explicit list, and buckets, edge functions and Vercel projects whose
# names start with DEMO_PREFIX.
DEMO_TABLES="customers waitlist"
DEMO_PREFIX=demo-

# Vars whose values must never be printed (dry runs mask them).
SECRET_VARS="SUPABASE_ACCESS_TOKEN MURETTO_SUPABASE_TOKEN SUPABASE_PUBLISHABLE_KEY SUPABASE_SECRET_KEY SUPABASE_SECRET_KEY_2 VERCEL_TOKEN MURETTO_VERCEL_TOKEN MURETTO_SYNC_TOKEN"

if [ -t 2 ] && [ -z "${NO_COLOR:-}" ]; then
	C_RED=$'\033[31m' C_GRN=$'\033[32m' C_YEL=$'\033[33m' C_DIM=$'\033[2m' C_B=$'\033[1m' C_0=$'\033[0m'
else
	C_RED='' C_GRN='' C_YEL='' C_DIM='' C_B='' C_0=''
fi

say() { printf '%s\n' "$*" >&2; }
step() { printf '%s==>%s %s\n' "$C_B" "$C_0" "$*" >&2; }
warn() { printf '%swarning:%s %s\n' "$C_YEL" "$C_0" "$*" >&2; }
die() {
	printf '%serror:%s %s\n' "$C_RED" "$C_0" "$*" >&2
	exit 1
}

# ---------------------------------------------------------------- .env

# load_env parses KEY=VALUE lines without executing anything (no `source`).
# Surrounding single or double quotes are stripped. File values win.
load_env() {
	[ -f "$DEMO_ENV_FILE" ] || die "$DEMO_ENV_FILE not found. Copy demo/.env.example to demo/.env and fill it in."
	local line key val n=0
	while IFS= read -r line || [ -n "$line" ]; do
		n=$((n + 1))
		line=${line%$'\r'}
		case $line in '' | '#'*) continue ;; esac
		case $line in *=*) ;; *) die "$DEMO_ENV_FILE:$n: expected KEY=VALUE" ;; esac
		key=${line%%=*}
		val=${line#*=}
		key=${key#export }
		key=$(printf '%s' "$key" | tr -d '[:space:]')
		printf '%s' "$key" | grep -Eq '^[A-Z_][A-Z0-9_]*$' || die "$DEMO_ENV_FILE:$n: bad variable name"
		case $val in
		\"*\") val=${val#\"} val=${val%\"} ;;
		\'*\') val=${val#\'} val=${val%\'} ;;
		esac
		export "$key=$val"
	done <"$DEMO_ENV_FILE"
	: "${DEMO_MODEL:=sonnet}" "${DEMO_MAX_BUDGET_USD:=2}" "${DEMO_SERVER_PORT:=8080}" "${DEMO_WEB_PORT:=3000}"
	: "${DEMO_ORG_NAME:=Acme (demo)}" "${DEMO_SYNC_INTERVAL:=5}" "${DEMO_REFRESH_INTERVAL:=20}"
	: "${SUPABASE_PROJECT_REF_2:=}" "${SUPABASE_SECRET_KEY:=}" "${SUPABASE_SECRET_KEY_2:=}"
	: "${MURETTO_SUPABASE_TOKEN:=}" "${MURETTO_VERCEL_TOKEN:=}"
	export DEMO_MODEL DEMO_MAX_BUDGET_USD DEMO_SERVER_PORT DEMO_WEB_PORT DEMO_ORG_NAME DEMO_SYNC_INTERVAL DEMO_REFRESH_INTERVAL
	SERVER_URL=http://127.0.0.1:$DEMO_SERVER_PORT
	WEB_URL=http://localhost:$DEMO_WEB_PORT
}

# require_demo refuses unless MURETTO_DEMO=1 and the config looks like a
# test setup. Call it after load_env.
require_demo() {
	if [ "${MURETTO_DEMO:-}" != 1 ]; then
		die "refusing to run: MURETTO_DEMO is not 1 in $DEMO_ENV_FILE. This harness creates and deletes real resources; use TEST accounts only, then set MURETTO_DEMO=1."
	fi
	local v
	for v in SUPABASE_PROJECT_REF SUPABASE_ACCESS_TOKEN SUPABASE_PUBLISHABLE_KEY VERCEL_TEAM_ID VERCEL_TEAM_SLUG VERCEL_TOKEN; do
		[ -n "$(eval "printf '%s' \"\${$v:-}\"")" ] || die "$v is empty in $DEMO_ENV_FILE"
	done
	# shellcheck disable=SC2153 # set by load_env
	check_ref "$SUPABASE_PROJECT_REF"
	[ -z "$SUPABASE_PROJECT_REF_2" ] || check_ref "$SUPABASE_PROJECT_REF_2"
	[ "$SUPABASE_PROJECT_REF_2" != "$SUPABASE_PROJECT_REF" ] || die "SUPABASE_PROJECT_REF_2 must differ from SUPABASE_PROJECT_REF (or be empty)"
	printf '%s' "$VERCEL_TEAM_ID" | grep -Eq '^team_[A-Za-z0-9]+$' || die "VERCEL_TEAM_ID must look like team_..."
	printf '%s' "$VERCEL_TEAM_SLUG" | grep -Eq '^[a-z0-9-]+$' || die "VERCEL_TEAM_SLUG must be a slug"
	printf '%s' "$VERCEL_TEAM_SLUG" | grep -Eiq 'demo|test' ||
		die "VERCEL_TEAM_SLUG '$VERCEL_TEAM_SLUG' does not contain 'demo' or 'test'. Use a dedicated test team."
	printf '%s' "$DEMO_MAX_BUDGET_USD" | grep -Eq '^[0-9]+(\.[0-9]+)?$' || die "DEMO_MAX_BUDGET_USD must be a number"
	printf '%s' "$DEMO_SERVER_PORT$DEMO_WEB_PORT$DEMO_SYNC_INTERVAL$DEMO_REFRESH_INTERVAL" | grep -Eq '^[0-9]+$' ||
		die "ports and intervals must be numbers"
	if [ -z "$MURETTO_SUPABASE_TOKEN" ]; then
		MURETTO_SUPABASE_TOKEN=$SUPABASE_ACCESS_TOKEN
		READ_TOKEN_REUSED=1
	fi
	[ -n "$MURETTO_VERCEL_TOKEN" ] || MURETTO_VERCEL_TOKEN=$VERCEL_TOKEN
	export MURETTO_SUPABASE_TOKEN MURETTO_VERCEL_TOKEN
}

check_ref() {
	printf '%s' "$1" | grep -Eq '^[a-z]{20}$' || die "'$1' is not a Supabase project ref (20 lowercase letters)"
}

# dev_ref <dev>: the test project <dev>'s session works in (carol and dan use
# the second project when there is one).
dev_ref() {
	case $1 in
	carol | dan) [ -z "$SUPABASE_PROJECT_REF_2" ] || { printf '%s' "$SUPABASE_PROJECT_REF_2" && return 0; } ;;
	esac
	printf '%s' "$SUPABASE_PROJECT_REF"
}

# project_refs prints the configured test project refs, one per line.
project_refs() {
	printf '%s\n' "$SUPABASE_PROJECT_REF"
	[ -z "$SUPABASE_PROJECT_REF_2" ] || printf '%s\n' "$SUPABASE_PROJECT_REF_2"
}

# confirm_ref "<what>" makes the human type the primary test project ref.
# DEMO_CONFIRM_REF=<ref> pre-confirms (for the 10-run reliability loop).
# Dry runs never ask.
confirm_ref() {
	[ "$DRY_RUN" = 1 ] && return 0
	if [ "${DEMO_CONFIRM_REF:-}" = "$SUPABASE_PROJECT_REF" ]; then
		say "Confirmed by DEMO_CONFIRM_REF."
		return 0
	fi
	say ""
	say "${C_B}$1${C_0}"
	say "Test accounts: Supabase project(s) $(project_refs | tr '\n' ' ')· Vercel team $VERCEL_TEAM_SLUG"
	local answer=''
	if [ -r /dev/tty ]; then
		printf 'Type the test project ref (%s) to continue: ' "$SUPABASE_PROJECT_REF" >/dev/tty
		IFS= read -r answer </dev/tty || true
	fi
	[ "$answer" = "$SUPABASE_PROJECT_REF" ] || die "confirmation did not match; nothing was done"
}

# ---------------------------------------------------------------- tools

tool_hint() {
	case $1 in
	claude) echo "Claude Code: https://docs.claude.com/en/docs/claude-code (e.g. npm install -g @anthropic-ai/claude-code)" ;;
	tmux) echo "tmux: brew install tmux  |  sudo apt-get install tmux" ;;
	jq) echo "jq: brew install jq  |  sudo apt-get install jq" ;;
	curl) echo "curl: brew install curl  |  sudo apt-get install curl" ;;
	go) echo "Go 1.27.3+: https://go.dev/dl/" ;;
	npm) echo "Node.js 20+ with npm: https://nodejs.org/" ;;
	vercel) echo "Vercel CLI (the agents deploy with it): npm install -g vercel" ;;
	uuidgen) echo "uuidgen: part of util-linux (Linux) or macOS" ;;
	muretto) echo "muretto from this repo: (cd $REPO_ROOT && make build)  ->  $MURETTO_BIN" ;;
	*) echo "$1" ;;
	esac
}

# check_tools <tool>... prints an install hint for each missing tool and
# fails. It never installs anything. In a dry run it only warns.
check_tools() {
	local t missing=0
	for t in "$@"; do
		if [ "$t" = muretto ]; then
			[ -x "$MURETTO_BIN" ] && continue
		elif command -v "$t" >/dev/null 2>&1; then
			continue
		fi
		missing=1
		if [ "$DRY_RUN" = 1 ]; then
			warn "missing $t (a real run stops here). Install: $(tool_hint "$t")"
		else
			say "${C_RED}missing:${C_0} $t. Install: $(tool_hint "$t")"
		fi
	done
	if [ "$missing" = 1 ] && [ "$DRY_RUN" != 1 ]; then
		die "install the missing tools above, then run again (this script never installs anything)"
	fi
	return 0
}

# clean_claude_env unsets variables inherited from a parent Claude Code
# session (CLAUDECODE, CLAUDE_CODE_*), so the headless sessions start clean.
clean_claude_env() {
	local v
	unset CLAUDECODE
	for v in $(env | sed -n 's/^\(CLAUDE_CODE_[A-Za-z0-9_]*\)=.*/\1/p'); do
		unset "$v"
	done
}

# ---------------------------------------------------------------- dry run

# mask replaces the value of every secret variable with ${NAME}.
mask() {
	local s=$1 v val
	for v in $SECRET_VARS; do
		val=$(eval "printf '%s' \"\${$v:-}\"")
		[ ${#val} -ge 6 ] || continue
		s=${s//"$val"/\$\{$v\}}
	done
	printf '%s' "$s"
}

# show_cmd prints a command, shell-quoted, with secrets masked.
show_cmd() {
	local out='' a
	for a in "$@"; do out="$out $(printf '%q' "$a")"; done
	printf '%s+%s%s\n' "$C_DIM" "$C_0" "$(mask "${out# }")" >&2
}

# run executes a command, or prints it in a dry run.
run() {
	if [ "$DRY_RUN" = 1 ]; then
		show_cmd "$@"
		return 0
	fi
	"$@"
}

# write_file <path> <mode> reads stdin into path (0600 by default); a dry run
# prints the content instead.
write_file() {
	local path=$1 mode=${2:-600} content
	content=$(cat)
	if [ "$DRY_RUN" = 1 ]; then
		{
			printf '%s+ write %s (mode %s):%s\n' "$C_DIM" "$path" "$mode" "$C_0"
			mask "$content" | sed 's/^/    /'
			printf '\n'
		} >&2
		return 0
	fi
	mkdir -p "$(dirname "$path")"
	(umask 077 && printf '%s\n' "$content" >"$path")
	chmod "$mode" "$path"
}

# ---------------------------------------------------------------- HTTP

# api <method> <url> <bearer-var> [json-body]: calls a provider API with the
# token taken from the named variable. Prints the body; fails on HTTP >= 400.
api() {
	local method=$1 url=$2 tokvar=$3 body=${4:-} tok out code
	tok=$(eval "printf '%s' \"\${$tokvar:-}\"")
	if [ "$DRY_RUN" = 1 ]; then
		if [ -n "$body" ]; then
			show_cmd curl -sS -X "$method" "$url" -H "Authorization: Bearer \${$tokvar}" -H 'Content-Type: application/json' --data "$body"
		else
			show_cmd curl -sS -X "$method" "$url" -H "Authorization: Bearer \${$tokvar}"
		fi
		return 0
	fi
	out=$(mktemp)
	if [ -n "$body" ]; then
		code=$(curl -sS -o "$out" -w '%{http_code}' -X "$method" "$url" \
			-H "Authorization: Bearer $tok" -H 'Content-Type: application/json' --data "$body") || code=000
	else
		code=$(curl -sS -o "$out" -w '%{http_code}' -X "$method" "$url" -H "Authorization: Bearer $tok") || code=000
	fi
	cat "$out"
	rm -f "$out"
	if [ "$code" -ge 400 ] 2>/dev/null || [ "$code" = 000 ]; then
		say "${C_RED}HTTP $code${C_0} from $method $url"
		return 1
	fi
}

# ---------------------------------------------------------------- local stack

dev_name() {
	printf '%s' "$1" | awk '{ print toupper(substr($0,1,1)) substr($0,2) }'
}
dev_home() { printf '%s/devs/%s/home' "$DEMO_RUN_DIR" "$1"; }
dev_token_file() { printf '%s/devs/%s/sync-token' "$DEMO_RUN_DIR" "$1"; }
dev_workdir() { printf '%s/work/%s' "$DEMO_RUN_DIR" "$1"; }

# start_server builds and starts murettod on 127.0.0.1 with dev login and the
# in-memory store (restarting it clears all server data).
start_server() {
	local bin=$DEMO_RUN_DIR/murettod log=$DEMO_RUN_DIR/server.log
	step "Starting the local sync server on $SERVER_URL (dev login, in-memory)"
	run go build -C "$REPO_ROOT" -o "$bin" ./server/cmd/murettod
	if [ "$DRY_RUN" = 1 ]; then
		show_cmd env -u MURETTO_DATABASE_URL MURETTO_DEV_LOGIN=1 "MURETTO_ADDR=127.0.0.1:$DEMO_SERVER_PORT" \
			"MURETTO_DASHBOARD_URL=$WEB_URL" "$bin"
		return 0
	fi
	if curl -fsS "$SERVER_URL/healthz" >/dev/null 2>&1; then
		die "something already answers on $SERVER_URL. Run demo/reset.sh --local, or change DEMO_SERVER_PORT."
	fi
	(
		unset MURETTO_DATABASE_URL MURETTO_GITHUB_CLIENT_ID MURETTO_GITHUB_CLIENT_SECRET PORT
		MURETTO_DEV_LOGIN=1 MURETTO_ADDR=127.0.0.1:$DEMO_SERVER_PORT MURETTO_DASHBOARD_URL=$WEB_URL \
			nohup "$bin" >"$log" 2>&1 &
		echo $! >"$DEMO_RUN_DIR/server.pid"
	)
		for _ in $(seq 1 50); do
		curl -fsS "$SERVER_URL/healthz" >/dev/null 2>&1 && return 0
		sleep 0.2
	done
	cat "$log" >&2
	die "the sync server did not become healthy (log above: $log)"
}

# start_web starts the Next.js dashboard against the local server.
start_web() {
	step "Starting the dashboard on $WEB_URL"
	if [ ! -d "$REPO_ROOT/web/node_modules" ]; then
		if [ "$DRY_RUN" = 1 ]; then
			warn "web/node_modules is missing (a real run would stop here). Run: (cd web && npm ci)"
		else
			die "web/node_modules is missing. Run: (cd $REPO_ROOT/web && npm ci). This script never installs anything."
		fi
	fi
	if [ "$DRY_RUN" = 1 ]; then
		show_cmd cd "$REPO_ROOT/web"
		show_cmd env "MURETTO_API_URL=$SERVER_URL" NEXT_PUBLIC_MURETTO_SAMPLE=0 npm run dev -- --port "$DEMO_WEB_PORT"
		return 0
	fi
	(
		cd "$REPO_ROOT/web" || exit 1
		MURETTO_API_URL=$SERVER_URL NEXT_PUBLIC_MURETTO_SAMPLE=0 nohup npm run dev -- --port "$DEMO_WEB_PORT" \
			>"$DEMO_RUN_DIR/web.log" 2>&1 &
		echo $! >"$DEMO_RUN_DIR/web.pid"
	)
		for _ in $(seq 1 120); do
		curl -fsS -o /dev/null "$WEB_URL/" 2>/dev/null && return 0
		sleep 0.5
	done
	warn "the dashboard is not answering yet; see $DEMO_RUN_DIR/web.log"
}

# dev_token <dev> asks the local server's dev-only endpoint for a CLI token
# and stores it 0600 in the run dir. It is a token for the local server only.
dev_token() {
	local dev=$1 file body
	file=$(dev_token_file "$dev")
	body=$(jq -cn --arg l "$dev" --arg n "$(dev_name "$dev")" '{login:$l, name:$n}')
	if [ "$DRY_RUN" = 1 ]; then
		show_cmd curl -fsS -X POST "$SERVER_URL/v1/dev/token" -H 'Content-Type: application/json' --data "$body"
		say "    -> access_token saved to $file (0600)"
		return 0
	fi
	mkdir -p "$(dirname "$file")"
	curl -fsS -X POST "$SERVER_URL/v1/dev/token" -H 'Content-Type: application/json' --data "$body" |
		jq -er .access_token | (umask 077 && cat >"$file") || die "could not get a dev token for $dev"
	chmod 600 "$file"
}

# Where the presenter's Claude Code writes transcripts (captured before any
# override). Each demo developer's muretto sees only its own session's
# project directory through a private CLAUDE_CONFIG_DIR of symlinks, so no
# developer picks up another's session or the presenter's own history.
CLAUDE_SRC_DIR=${CLAUDE_CONFIG_DIR:-$HOME/.claude}
dev_claude_dir() { printf '%s/devs/%s/claude' "$DEMO_RUN_DIR" "$1"; }

# claude_project_name <path>: Claude Code's project dir name for a cwd
# ("non-alphanumeric characters replaced by -", research §2.1).
claude_project_name() { printf '%s' "$1" | sed 's/[^A-Za-z0-9]/-/g'; }

# link_transcripts <dev>: links <dev>'s Claude project dir (both the logical
# and the physical spelling of the workdir, e.g. /var vs /private/var).
link_transcripts() {
	local dev=$1 work dir p name phys
	work=$(dev_workdir "$dev")
	dir=$(dev_claude_dir "$dev")/projects
	run mkdir -p "$dir"
	phys=$( (cd "$work" 2>/dev/null && pwd -P) || printf '%s' "$work")
	for p in "$work" "$phys"; do
		# The second pass is only needed when the physical path differs.
		if [ "$p" = "$phys" ] && [ "$phys" = "$work" ] && [ -n "${name:-}" ]; then continue; fi
		name=$(claude_project_name "$p")
		run ln -sfn "$CLAUDE_SRC_DIR/projects/$name" "$dir/$name"
	done
}

# muretto_as <dev> <args...> runs the repo-built muretto as that developer.
muretto_as() {
	local dev=$1
	shift
	if [ "$DRY_RUN" = 1 ]; then
		show_cmd env "MURETTO_HOME=$(dev_home "$dev")" "CLAUDE_CONFIG_DIR=$(dev_claude_dir "$dev")" \
			"MURETTO_SYNC_TOKEN=\$(cat $(dev_token_file "$dev"))" \
			"MURETTO_SUPABASE_TOKEN=\${MURETTO_SUPABASE_TOKEN}" "MURETTO_VERCEL_TOKEN=\${MURETTO_VERCEL_TOKEN}" \
			"$MURETTO_BIN" "$@"
		return 0
	fi
	local tok=''
	[ -f "$(dev_token_file "$dev")" ] && tok=$(cat "$(dev_token_file "$dev")")
	MURETTO_HOME=$(dev_home "$dev") CLAUDE_CONFIG_DIR=$(dev_claude_dir "$dev") MURETTO_SYNC_TOKEN=$tok "$MURETTO_BIN" "$@"
}

# setup_dev <dev>: own MURETTO_HOME, providers connected from the env tokens
# (Muretto lists accounts read-only), sync pointed at the local server, on.
setup_dev() {
	local dev=$1 home cfg
	home=$(dev_home "$dev")
	cfg=$home/config.json
	step "Developer $dev: MURETTO_HOME=$home"
	run mkdir -p "$home" "$(dev_workdir "$dev")"
	link_transcripts "$dev"
	dev_token "$dev"
	muretto_as "$dev" init --yes --no-hooks --name "$(dev_name "$dev")" >&2
	if [ "$DRY_RUN" = 1 ]; then
		# shellcheck disable=SC2016 # jq program, printed verbatim
		show_cmd jq --arg s "$SERVER_URL" --arg l "$dev" '.sync.server=$s | .developer.account_login=$l' "$cfg"
	else
		local tmp
		tmp=$(mktemp "$home/.cfg.XXXXXX")
		jq --arg s "$SERVER_URL" --arg l "$dev" '.sync.server=$s | .developer.account_login=$l' "$cfg" >"$tmp" &&
			chmod 600 "$tmp" && mv "$tmp" "$cfg"
	fi
	muretto_as "$dev" sync on --yes >/dev/null
}

# cookie_login <dev> <jar>: dev-only browser login; prints the CSRF token.
cookie_login() {
	local dev=$1 jar=$2
	if [ "$DRY_RUN" = 1 ]; then
		show_cmd curl -fsS -c "$jar" -o /dev/null "$SERVER_URL/auth/dev/login?login=$dev&name=$(dev_name "$dev")&return_to=/app"
		show_cmd curl -fsS -b "$jar" "$SERVER_URL/v1/me"
		echo csrf-placeholder
		return 0
	fi
	curl -fsS -c "$jar" -o /dev/null "$SERVER_URL/auth/dev/login?login=$dev&name=$(dev_name "$dev")&return_to=/app" ||
		die "dev login failed for $dev"
	curl -fsS -b "$jar" "$SERVER_URL/v1/me" | jq -er .csrf_token
}

# cookie_post <jar> <csrf> <path> <json>
cookie_post() {
	if [ "$DRY_RUN" = 1 ]; then
		show_cmd curl -fsS -b "$1" -X POST "$SERVER_URL$3" -H "X-CSRF-Token: <csrf>" -H 'Content-Type: application/json' --data "$4"
		echo '{}'
		return 0
	fi
	curl -fsS -b "$1" -X POST "$SERVER_URL$3" -H "X-CSRF-Token: $2" -H 'Content-Type: application/json' --data "$4"
}

# setup_org <owner> <member>...: the owner creates the demo team and every
# member joins with a single-use invite, so the Team view shows everyone.
setup_org() {
	local owner=$1 jar csrf org_id url token m
	shift
	step "Creating the team '$DEMO_ORG_NAME' (owner $owner) and inviting: $*"
	jar=$DEMO_RUN_DIR/cookies-$owner.txt
	csrf=$(cookie_login "$owner" "$jar")
	org_id=$(cookie_post "$jar" "$csrf" /v1/orgs "$(jq -cn --arg n "$DEMO_ORG_NAME" '{name:$n}')" | jq -r '.id // "org_placeholder"')
	[ -n "$org_id" ] && [ "$org_id" != null ] || die "could not create the team"
	[ "$DRY_RUN" = 1 ] || printf '%s\n' "$org_id" >"$DEMO_RUN_DIR/org-id"
	for m in "$@"; do
		url=$(cookie_post "$jar" "$csrf" "/v1/orgs/$org_id/invites" '{}' | jq -r '.url // "/join/mri_placeholder"')
		token=${url##*/join/}
		local mjar=$DEMO_RUN_DIR/cookies-$m.txt mcsrf
		mcsrf=$(cookie_login "$m" "$mjar")
		cookie_post "$mjar" "$mcsrf" "/v1/invites/$token/accept" '{}' >/dev/null || die "$m could not join the team"
	done
	[ "$DRY_RUN" = 1 ] || rm -f "$DEMO_RUN_DIR"/cookies-*.txt
}

# hooks_settings prints the per-run --settings file: Muretto's hook events
# (internal/claudecfg.Events) in exec form, pointing at the repo-built binary,
# and approval for the "supabase" server in each workdir's .mcp.json.
hooks_settings() {
	jq -n --arg bin "$MURETTO_BIN" '
	  def h($e): {type:"command", command:$bin, args:["hook",$e], timeout:5};
	  {enabledMcpjsonServers: ["supabase"],
	   hooks: {
	    SessionStart:       [{hooks:[h("SessionStart")]}],
	    UserPromptSubmit:   [{hooks:[h("UserPromptSubmit")]}],
	    PostToolUse:        [{matcher:"*", hooks:[h("PostToolUse")]}],
	    PostToolUseFailure: [{matcher:"*", hooks:[h("PostToolUseFailure")]}],
	    SubagentStart:      [{hooks:[h("SubagentStart")]}],
	    SessionEnd:         [{hooks:[h("SessionEnd")]}]
	  }}'
}

# mcp_config <ref>: a .mcp.json with the Supabase MCP server pinned to one
# test project. It goes into the developer's workdir (project scope), so the
# hook can read the pinned project_ref and Muretto can attribute (decision
# 0905). The token is expanded by Claude Code from the environment, never
# written here.
mcp_config() {
	jq -n --arg ref "$1" '{mcpServers: {supabase: {
	  type: "http",
	  url: ("https://mcp.supabase.com/mcp?project_ref=" + $ref + "&features=database,functions,development"),
	  headers: {Authorization: "Bearer ${SUPABASE_ACCESS_TOKEN}"}
	}}}'
}

# stop_pidfile <file>: stops the process recorded in a pid file, if any.
stop_pidfile() {
	local f=$1 pid
	[ -f "$f" ] || return 0
	pid=$(cat "$f")
	if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
		kill_tree "$pid"
	fi
	run rm -f "$f"
}

# kill_tree <pid>: stops a process and its descendants (npm -> next -> node).
kill_tree() {
	local child
	for child in $(pgrep -P "$1" 2>/dev/null || true); do
		kill_tree "$child"
	done
	run kill "$1" 2>/dev/null || true
}

# ---------------------------------------------------------------- providers
# Only the demo scripts call these, and only against the TEST accounts in
# demo/.env. Muretto itself goes through its read-only broker instead.

SUPABASE_API=https://api.supabase.com
VERCEL_API=https://api.vercel.com

# assert_test_projects refuses unless every configured project's name
# contains "demo" or "test" (one read: GET /v1/projects).
assert_test_projects() {
	local list ref name
	if [ "$DRY_RUN" = 1 ]; then
		api GET "$SUPABASE_API/v1/projects" SUPABASE_ACCESS_TOKEN
		say "    -> refuse unless each of: $(project_refs | tr '\n' ' ')is listed with 'demo' or 'test' in its name"
		return 0
	fi
	list=$(api GET "$SUPABASE_API/v1/projects" SUPABASE_ACCESS_TOKEN) || die "could not list Supabase projects with SUPABASE_ACCESS_TOKEN"
	for ref in $(project_refs); do
		name=$(printf '%s' "$list" | jq -r --arg r "$ref" '(if type == "array" then . else .projects // [] end)[] | select(.ref == $r) | .name')
		[ -n "$name" ] || die "project $ref is not visible to SUPABASE_ACCESS_TOKEN"
		printf '%s' "$name" | grep -Eiq 'demo|test' ||
			die "project $ref is named '$name'. Refusing: the demo only runs on projects whose name contains 'demo' or 'test'."
		say "  Supabase project $ref: '$name' (test project) ✓"
	done
}

# sql <ref> <query>: runs SQL on a TEST project through the Management API
# (POST /v1/projects/{ref}/database/query, a write endpoint; demo use only).
sql() {
	api POST "$SUPABASE_API/v1/projects/$1/database/query" SUPABASE_ACCESS_TOKEN "$(jq -cn --arg q "$2" '{query:$q}')"
}

# is_demo_name <name>: only names with the demo prefix may be deleted.
is_demo_name() {
	case $1 in "$DEMO_PREFIX"?*) printf '%s' "$1" | grep -Eq '^[a-z0-9][a-z0-9._-]*$' ;; *) return 1 ;; esac
}
