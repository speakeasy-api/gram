-- atlas:txmode none

-- Drop the legacy `resource` column from principal_grants.
-- All grants now use the `selectors` JSONB column exclusively.

-- Backfill any rows that still have NULL selectors (created before the selectors
-- column was populated by application code). Derive resource_kind from the scope
-- prefix and use the existing resource column as resource_id.
UPDATE principal_grants
SET selectors = jsonb_build_object(
  'resource_kind', CASE
    WHEN scope LIKE 'project:%' THEN 'project'
    WHEN scope LIKE 'build:%' THEN 'project'
    WHEN scope LIKE 'mcp:%' THEN 'mcp'
    WHEN scope LIKE 'remote-mcp:%' THEN 'mcp'
    WHEN scope LIKE 'org:%' THEN 'org'
    ELSE '*'
  END,
  'resource_id', COALESCE(resource, '*')
)
WHERE selectors IS NULL;

-- Make selectors NOT NULL (table is small — full scan acceptable).
-- atlas:nolint PG303
ALTER TABLE principal_grants ALTER COLUMN selectors SET NOT NULL;

-- Tighten CHECK constraint (old one allowed NULL). Use NOT VALID + VALIDATE
-- to avoid holding ACCESS EXCLUSIVE lock during the scan.
ALTER TABLE principal_grants DROP CONSTRAINT IF EXISTS principal_grants_selectors_check;
ALTER TABLE principal_grants ADD CONSTRAINT principal_grants_selectors_check CHECK (jsonb_typeof(selectors) = 'object' AND selectors != '{}') NOT VALID;
ALTER TABLE principal_grants VALIDATE CONSTRAINT principal_grants_selectors_check;

-- Drop the resource-based unique index concurrently.
DROP INDEX CONCURRENTLY IF EXISTS principal_grants_org_principal_scope_unrestricted_key;

-- Drop partial selector unique index concurrently, recreate as non-partial.
DROP INDEX CONCURRENTLY IF EXISTS principal_grants_org_principal_scope_selector_key;
CREATE UNIQUE INDEX CONCURRENTLY principal_grants_org_principal_scope_selector_key
ON principal_grants (organization_id, principal_urn, scope, selectors);

-- Drop and recreate GIN index concurrently without partial WHERE clause.
DROP INDEX CONCURRENTLY IF EXISTS principal_grants_selectors_idx;
CREATE INDEX CONCURRENTLY principal_grants_selectors_idx
ON principal_grants USING GIN (selectors);

-- Drop the resource column. This is the "contract" step of expand-contract:
-- PR #2357 was the expand (added selectors, stopped reading/writing resource).
-- atlas:nolint DS103
ALTER TABLE principal_grants DROP COLUMN resource;

-- Update comments to reflect the new schema (selectors is now the sole mechanism).
COMMENT ON TABLE principal_grants IS 'RBAC grants. One row per (org, principal, scope, selectors). Selectors define resource constraints.';
COMMENT ON COLUMN principal_grants.selectors IS 'JSON selector constraints defining what the grant applies to, e.g. {"resource_kind":"project","resource_id":"proj_123"}.';
