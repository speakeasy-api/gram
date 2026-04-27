-- atlas:txmode none

-- Drop the legacy `resource` column from principal_grants.
-- All grants now use the `selectors` JSONB column exclusively.

-- Make selectors NOT NULL.
ALTER TABLE principal_grants ALTER COLUMN selectors SET NOT NULL;

-- Tighten CHECK constraint (old one allowed NULL).
ALTER TABLE principal_grants DROP CONSTRAINT IF EXISTS principal_grants_selectors_check;
ALTER TABLE principal_grants ADD CONSTRAINT principal_grants_selectors_check CHECK (jsonb_typeof(selectors) = 'object' AND selectors != '{}');

-- Drop the resource-based unique index.
DROP INDEX IF EXISTS principal_grants_org_principal_scope_unrestricted_key;

-- Drop partial selector unique index, recreate as non-partial.
DROP INDEX IF EXISTS principal_grants_org_principal_scope_selector_key;
CREATE UNIQUE INDEX CONCURRENTLY principal_grants_org_principal_scope_selector_key
ON principal_grants (organization_id, principal_urn, scope, selectors);

-- Drop and recreate GIN index without partial WHERE clause.
DROP INDEX IF EXISTS principal_grants_selectors_idx;
CREATE INDEX CONCURRENTLY principal_grants_selectors_idx
ON principal_grants USING GIN (selectors);

-- Drop the resource column.
ALTER TABLE principal_grants DROP COLUMN resource;
