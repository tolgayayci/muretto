# examples/live-demo/pretty.jq: turns `claude -p --output-format stream-json --verbose
# --include-hook-events` lines into a calm, readable pane for the video.
# Use with: jq --unbuffered -R -r -f examples/live-demo/pretty.jq
# Non-JSON lines pass through. Token-shaped strings are masked.

def esc: "\u001b[";
def dim: esc + "2m" + . + esc + "0m";
def bold: esc + "1m" + . + esc + "0m";
def cyan: esc + "36m" + . + esc + "0m";
def green: esc + "32m" + . + esc + "0m";
def red: esc + "31m" + . + esc + "0m";
def yellow: esc + "33m" + . + esc + "0m";

def mask:
  gsub("sbp_[A-Za-z0-9]{8,}"; "sbp_…")
  | gsub("sb_secret_[A-Za-z0-9_-]{8,}"; "sb_secret_…")
  | gsub("sb_publishable_[A-Za-z0-9_-]{8,}"; "sb_publishable_…")
  | gsub("vc[pkira]_[A-Za-z0-9]{8,}"; "vcp_…")
  | gsub("mrt_[A-Za-z0-9_-]{8,}"; "mrt_…")
  | gsub("eyJ[A-Za-z0-9_-]{8,}\\.[A-Za-z0-9_-]{8,}\\.[A-Za-z0-9_-]+"; "eyJ…");

def one_line($n):
  tostring | gsub("[\\s]+"; " ") | ltrimstr(" ")
  | if length > $n then .[0:$n] + "…" else . end;

def short_tool:
  sub("^mcp__supabase__"; "supabase.") | sub("^mcp__(?<s>[^_]+)__"; "\(.s).");

def tool_arg:
  if .name == "Bash" then .input.command // ""
  elif (.name | test("execute_sql|apply_migration")) then .input.query // ""
  elif (.name | test("deploy_edge_function")) then "function " + (.input.name // "?")
  elif (.input.file_path? // null) != null then .input.file_path
  else (.input | tostring)
  end;

def result_text:
  if (.content | type) == "string" then .content
  elif (.content | type) == "array" then [.content[] | (.text? // "")] | join(" ")
  else "" end;

def render:
  if .type == "system" and .subtype == "init" then
    ("● session " + ((.session_id // "")[0:8]) + " · " + (.model // "?")
     + " · mcp: " + ([.mcp_servers[]? | .name + "=" + .status] | join(", "))) | dim
  elif .type == "system" and (.subtype == "hook_response") then
    if (.exit_code // 0) == 0 then empty
    else ("  ⚑ hook " + (.hook_name // "?") + " exit " + ((.exit_code // "?") | tostring)) | yellow end
  elif .type == "system" and (.subtype == "hook_started") then
    if ((.hook_name // "") | test("SessionStart")) then "  ⚑ muretto hooks active" | dim else empty end
  elif .type == "system" then empty
  elif .type == "assistant" then
    .message.content[]?
    | if .type == "text" then (.text | one_line(220) | mask)
      elif .type == "tool_use" then
        ("▸ " | cyan) + (.name | short_tool | bold) + " " + (tool_arg | one_line(160) | mask)
      else empty end
  elif .type == "user" then
    .message.content[]?
    | select(type == "object" and .type == "tool_result")
    | if .is_error == true then ("  ✗ " + (result_text | one_line(140) | mask)) | red
      else ("  ✓ " + (result_text | one_line(140) | mask)) | dim end
  elif .type == "result" then
    (if .is_error == true or .subtype != "success" then ("■ " + (.subtype // "error")) | red
     else "■ done" | green end) as $head
    | ("  " + ((.num_turns // 0) | tostring) + " turns · "
       + (((.duration_ms // 0) / 1000 | floor) | tostring) + "s · $"
       + (((.total_cost_usd // 0) * 100 | round / 100) | tostring) | dim) as $tail
    | "", $head + $tail
  else empty end;

. as $line
| (try ($line | fromjson) catch null) as $msg
| if $msg == null then ($line | mask)
  elif ($msg | type) != "object" then ($line | mask)
  else ($msg | render) end
