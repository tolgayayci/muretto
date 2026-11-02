-- Dashboard sign-in moved to Supabase Auth: the dashboard sends the user's
-- Supabase access token as a Bearer token and nothing is stored for it. The
-- cookie sessions of the retired GitHub OAuth login are dead credentials now.
drop table if exists web_sessions;

-- GET /v1/tokens lists a user's active CLI tokens, newest first.
create index if not exists api_tokens_user_active_idx on api_tokens (user_id, created_at desc) where revoked_at is null;
