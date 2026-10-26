-- Muretto sync server schema. Secrets are stored only as sha256 hashes.

create table users (
    id           text primary key,
    identity     text not null unique,          -- github:<numeric id> or dev:<login>
    login        text not null,
    display_name text not null,
    created_at   timestamptz not null
);

create table web_sessions (
    token_hash text primary key,
    user_id    text not null references users (id) on delete cascade,
    created_at timestamptz not null,
    expires_at timestamptz not null
);
create index web_sessions_expires_idx on web_sessions (expires_at);

create table api_tokens (
    id           text primary key,
    token_hash   text not null unique,
    user_id      text not null references users (id) on delete cascade,
    name         text not null,
    created_at   timestamptz not null,
    last_used_at timestamptz,
    revoked_at   timestamptz
);

create table device_auths (
    device_code_hash text primary key,
    user_code        text not null unique,
    client_info      jsonb not null,
    status           text not null check (status in ('pending', 'approved', 'denied', 'consumed')),
    user_id          text references users (id) on delete cascade,
    interval_ms      bigint not null,
    created_at       timestamptz not null,
    expires_at       timestamptz not null,
    last_poll_at     timestamptz
);

create table orgs (
    id         text primary key,
    name       text not null,
    created_at timestamptz not null
);

create table memberships (
    org_id    text not null references orgs (id) on delete cascade,
    user_id   text not null references users (id) on delete cascade,
    role      text not null check (role in ('owner', 'member')),
    joined_at timestamptz not null,
    primary key (org_id, user_id)
);
create index memberships_user_idx on memberships (user_id);

create table invites (
    token_hash text primary key,
    org_id     text not null references orgs (id) on delete cascade,
    created_by text not null references users (id) on delete cascade,
    created_at timestamptz not null,
    expires_at timestamptz not null,
    used_at    timestamptz,
    used_by    text references users (id) on delete set null
);

create table entities (
    user_id    text not null references users (id) on delete cascade,
    kind       text not null,
    key        text not null,
    rev        text not null,
    data       jsonb not null,
    updated_at timestamptz not null,
    primary key (user_id, kind, key)
);

create table events (
    seq        bigserial primary key,
    user_id    text not null references users (id) on delete cascade,
    type       text not null,
    entity_id  text not null,
    developer  text not null,
    created_at timestamptz not null
);
create index events_user_seq_idx on events (user_id, seq);

create table provider_requests (
    id         bigserial primary key,
    provider   text not null,
    email      text,
    created_at timestamptz not null
);
