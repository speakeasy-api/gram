-- Drop the old composite unique constraint that prevents multiple selectors
-- with the same resource_id (e.g. two tools on the same MCP server).
-- The selectors JSONB column is now the source of truth for grant identity.
ALTER TABLE principal_grants
  DROP CONSTRAINT IF EXISTS principal_grants_org_principal_scope_resource_key;
