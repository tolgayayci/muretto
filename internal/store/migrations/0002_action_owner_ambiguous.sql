-- Set when two sessions hold the same tool call with identical timing, so the
-- owner cannot be told apart. Attribution then treats its resources as unknown.
ALTER TABLE actions ADD COLUMN owner_ambiguous INTEGER NOT NULL DEFAULT 0;
