-- Who asked for a provider, when a signed-in dashboard user did. Landing page
-- requests stay anonymous (user_id null).
alter table provider_requests add column user_id text references users (id) on delete set null;
