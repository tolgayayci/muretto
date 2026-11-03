# What `muretto sync` sends

Sync is **off by default**. `muretto login` and `muretto sync on` are explicit opt-ins, and `sync on` prints this list before enabling. Everything works locally without an account.

## What is never sent

- **Tokens or keys**: no provider tokens, no Muretto token (it travels only in the `Authorization` header), no secret values.
- **Local paths**: the working directory is sent only as a hash; home-directory paths inside any text become `~`; a git remote that is a local path is dropped.
- **Raw transcripts and full prompts**: only a short task title (at most 80 characters) and short evidence excerpts (at most 1000 characters).
- **Raw tool input or output**, agent actions that created nothing, warnings, and the summary (the server recomputes it).

Every string is redacted again (`internal/redact`, the same redactor the hook uses) right before upload, Muretto's own `mrt_`/`mrs_`/`mri_` tokens are removed, URLs lose credentials, query strings and fragments, and resource attributes under secret-looking keys (password, secret, token, api_key, private_key, authorization, credential, cookie, connection_string, dsn) are replaced by `[REDACTED]`.

## How it is sent

- `POST /v1/ingest` over HTTPS (plain HTTP only to `localhost`), as one JSON `SyncBatch` (`schema/sync-batch.schema.json`). The server validates it against the schema and rejects anything else.
- Each entity carries a `rev`: the sha256 of its JSON. Only entities whose `rev` changed since the last successful sync are sent, and the server ignores a record whose `rev` it already has.
- Data belongs to the user who synced it. The dashboard shows only your own data today. The server's API also has a team view (members of an org you joined see your data there); the dashboard does not show it yet.
- Sync runs when a Muretto command runs, and on `muretto sync now`. There is no daemon.

## Field by field

<!-- payload:begin (generated from syncclient.Payload(); run MURETTO_UPDATE_SYNC_PAYLOAD=1 go test ./internal/syncclient) -->
| Entity | Field | What is sent |
|---|---|---|
| batch | `schema_version` | Always 1 |
| batch | `batch_id` | Random id for this upload |
| batch | `sent_at` | When the batch was built (UTC) |
| batch | `client_version` | Muretto CLI version |
| developer | `id` | Local developer id (a hash, or your linked account id) |
| developer | `display_name` | Your display name. Redacted, home-directory paths replaced by ~ |
| developer | `machine_id_hash` | Salted hash of the machine id; the raw id never leaves the machine |
| developer | `account_login` | Your GitHub login, once logged in |
| session | `rev` | Content hash (sha256 of the entity's JSON). Same rev means unchanged, so it is not re-sent |
| session | `id` | Claude Code session id (a UUID) |
| session | `agent` | Always claude-code |
| session | `agent_version` | Claude Code version |
| session | `model` | Model name |
| session | `developer_id` | Local developer id |
| session | `repo.path_hash` | Hash of the working directory. The path itself is never sent |
| session | `repo.remote_url` | Git remote URL without credentials, query or fragment; dropped if it is a local path |
| session | `repo.display_name` | owner/repo, or the directory's base name (never a full path) |
| session | `git_branch` | Branch name. Redacted, home-directory paths replaced by ~ |
| session | `started_at` | Timestamp |
| session | `ended_at` | Timestamp |
| session | `last_activity_at` | Timestamp |
| session | `task_title` | Short task title, at most 80 characters. Redacted, home-directory paths replaced by ~. Never the full prompt |
| session | `title_source` | custom_title, first_prompt, ai_title or none |
| session | `sources` | hook and/or transcript |
| session | `end_reason` | Why the session ended, e.g. logout |
| resource | `rev` | Content hash (sha256 of the entity's JSON). Same rev means unchanged, so it is not re-sent |
| resource | `id` | `<kind>:<provider_id>` |
| resource | `provider` | Provider slug, e.g. supabase |
| resource | `account_id` | Provider org/team id |
| resource | `account_name` | Provider org/team name. Redacted, home-directory paths replaced by ~ |
| resource | `kind` | e.g. supabase.table |
| resource | `provider_id` | Provider id or natural key |
| resource | `name` | Resource name. Redacted, home-directory paths replaced by ~ |
| resource | `parent_id` | Parent resource id |
| resource | `url` | Provider URL without credentials, query or fragment |
| resource | `status` | Provider status, e.g. active |
| resource | `created_at` | Creation time reported by the provider |
| resource | `first_seen_at` | Timestamp |
| resource | `last_seen_at` | Timestamp |
| resource | `existence` | confirmed, unconfirmed or gone |
| resource | `gone_at` | Timestamp |
| resource | `seen_via` | list, advisor or evidence |
| resource | `attributes` | Scalar attributes such as region or public. String values redacted; values under secret-looking keys replaced by [REDACTED] |
| attribution | `rev` | Content hash (sha256 of the entity's JSON). Same rev means unchanged, so it is not re-sent |
| attribution | `resource_id` | Resource id |
| attribution | `label` | exact or unknown |
| attribution | `session_id` | The creating session (exact only) |
| attribution | `rule_id` | Attribution rule that fired |
| attribution | `reason` | Plain-language reason. Redacted, home-directory paths replaced by ~ |
| attribution | `evidence[].action_id` | Claude Code tool_use_id |
| attribution | `evidence[].session_id` | Session id |
| attribution | `evidence[].occurred_at` | Timestamp |
| attribution | `evidence[].tool_name` | e.g. Bash or an MCP tool name |
| attribution | `evidence[].signature_id` | Adapter signature that matched |
| attribution | `evidence[].matched_value` | The identifier that matched, e.g. a project ref. Redacted, home-directory paths replaced by ~ |
| attribution | `evidence[].excerpt` | Short excerpt of the command or result, at most 1000 characters. Redacted, home-directory paths replaced by ~ |
| attribution | `computed_at` | Timestamp |
| finding | `rev` | Content hash (sha256 of the entity's JSON). Same rev means unchanged, so it is not re-sent |
| finding | `id` | `<rule_id>:<resource_id>` |
| finding | `resource_id` | Resource id |
| finding | `rule_id` | e.g. rls_disabled |
| finding | `state` | exposed, not_exposed or unknown |
| finding | `severity` | critical, high, medium, low or info |
| finding | `title` | Redacted, home-directory paths replaced by ~ |
| finding | `explanation` | Redacted, home-directory paths replaced by ~ |
| finding | `caveat` | Redacted, home-directory paths replaced by ~ |
| finding | `checked_at` | Timestamp |
| finding | `signal` | What the finding is based on |
| cost | `rev` | Content hash (sha256 of the entity's JSON). Same rev means unchanged, so it is not re-sent |
| cost | `resource_id` | Resource id |
| cost | `source` | reported, estimated or unknown |
| cost | `amount_micros` | Amount in millionths of the currency unit; absent when unknown |
| cost | `currency` | e.g. USD |
| cost | `interval` | hourly, monthly or period |
| cost | `period_start` | Timestamp |
| cost | `period_end` | Timestamp |
| cost | `as_of` | Timestamp |
| cost | `basis` | How the number was obtained. Redacted, home-directory paths replaced by ~ |
| review | `rev` | Content hash (sha256 of the entity's JSON). Same rev means unchanged, so it is not re-sent |
| review | `resource_id` | Resource id |
| review | `owner_developer_id` | Developer whose session created it |
| review | `due_at` | Timestamp |
| review | `state` | pending, keep or not_needed |
| review | `decided_by` | Developer id |
| review | `decided_at` | Timestamp |
| review | `note` | Your note. Redacted, home-directory paths replaced by ~ |
| check | `rev` | Content hash (sha256 of the entity's JSON). Same rev means unchanged, so it is not re-sent |
| check | `provider` | Provider slug |
| check | `account_id` | Provider org/team id |
| check | `kind` | Resource kind checked |
| check | `scope` | Narrower scope, e.g. a project ref |
| check | `rule_id` | Exposure rule evaluated |
| check | `status` | ok, error or skipped |
| check | `attempted_at` | Timestamp |
| check | `last_success_at` | Timestamp |
| check | `items` | How many items the check saw |
| check | `error_code` | auth, permission, not_found, rate_limit, network, provider or unsupported |
| check | `error` | Error message. Redacted, home-directory paths replaced by ~ |
<!-- payload:end -->

A test (`internal/syncclient/payload_test.go`) fails if this table, `syncclient.Payload()` and the `SyncBatch` Go type ever disagree.
