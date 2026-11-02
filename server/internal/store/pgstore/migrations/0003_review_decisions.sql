-- "Still need this?" decisions made in the dashboard (Keep / Not needed /
-- undo), one per user and resource. The answer view merges them with the
-- reviews the CLI syncs: the later decided_at wins.
create table review_decisions (
    user_id     text not null references users (id) on delete cascade,
    resource_id text not null,
    state       text not null check (state in ('keep', 'not_needed', 'pending')),
    note        text,
    decided_at  timestamptz not null,
    primary key (user_id, resource_id)
);
