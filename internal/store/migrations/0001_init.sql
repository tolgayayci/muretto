-- Muretto local store, version 1. Entities are stored as their contract JSON,
-- with the columns needed for lookups next to them.

CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,
    started_at  TEXT NOT NULL,
    developer_id TEXT NOT NULL,
    body        TEXT NOT NULL
);

CREATE TABLE actions (
    id          TEXT PRIMARY KEY,           -- Claude Code tool_use_id
    session_id  TEXT NOT NULL,
    occurred_at TEXT NOT NULL,
    source      TEXT NOT NULL,
    body        TEXT NOT NULL,              -- contract.AgentAction with identifiers
    raw_input   TEXT,                       -- redacted tool input (JSON), for re-extraction
    raw_output  TEXT,                       -- redacted result text, for re-extraction
    extracted   INTEGER NOT NULL DEFAULT 0  -- 1 once adapter signatures ran
);
CREATE INDEX actions_session ON actions(session_id);

CREATE TABLE resources (
    id          TEXT PRIMARY KEY,
    provider    TEXT NOT NULL,
    account_id  TEXT NOT NULL,
    kind        TEXT NOT NULL,
    scope       TEXT NOT NULL,              -- parent provider id, or '' at account level
    existence   TEXT NOT NULL,
    body        TEXT NOT NULL
);
CREATE INDEX resources_account ON resources(provider, account_id);

CREATE TABLE resource_events (
    resource_id TEXT NOT NULL,
    at          TEXT NOT NULL,
    event       TEXT NOT NULL               -- seen, gone, returned, unconfirmed
);
CREATE INDEX resource_events_resource ON resource_events(resource_id);

CREATE TABLE checks (
    provider    TEXT NOT NULL,
    account_id  TEXT NOT NULL,
    kind        TEXT NOT NULL,
    scope       TEXT NOT NULL,
    rule_id     TEXT NOT NULL,
    body        TEXT NOT NULL,              -- latest attempt, last_success_at carried forward
    PRIMARY KEY (provider, account_id, kind, scope, rule_id)
);

CREATE TABLE findings (
    id          TEXT PRIMARY KEY,
    provider    TEXT NOT NULL,
    account_id  TEXT NOT NULL,
    kind        TEXT NOT NULL,
    scope       TEXT NOT NULL,
    rule_id     TEXT NOT NULL,
    body        TEXT NOT NULL
);

CREATE TABLE costs (
    resource_id TEXT NOT NULL,
    source      TEXT NOT NULL,
    interval    TEXT NOT NULL,
    provider    TEXT NOT NULL,
    account_id  TEXT NOT NULL,
    body        TEXT NOT NULL,
    PRIMARY KEY (resource_id, source, interval)
);

CREATE TABLE reviews (
    resource_id TEXT PRIMARY KEY,
    body        TEXT NOT NULL               -- contract.ReviewItem with a human decision
);

CREATE TABLE sync_revs (
    key         TEXT PRIMARY KEY,
    rev         TEXT NOT NULL
);

CREATE TABLE meta (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL
);
