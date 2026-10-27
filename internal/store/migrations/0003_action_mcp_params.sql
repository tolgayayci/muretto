-- Identifier params (e.g. project_ref) of the MCP server a hook-recorded call
-- went to, as a JSON object of strings, for re-extraction. NULL when none.
ALTER TABLE actions ADD COLUMN mcp_params TEXT;
