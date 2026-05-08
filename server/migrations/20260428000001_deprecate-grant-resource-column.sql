-- atlas:txmode none

-- Soft-deprecate the resource column: make nullable, drop default, rename with
-- drop_ prefix to signal it is scheduled for removal.

ALTER TABLE principal_grants ALTER COLUMN resource DROP NOT NULL;
ALTER TABLE principal_grants ALTER COLUMN resource DROP DEFAULT;
ALTER TABLE principal_grants RENAME COLUMN resource TO drop_resource;

-- Consolidate the two partial unique indexes into a single non-partial index.
-- The old pair (one WHERE selectors IS NOT NULL, one WHERE selectors IS NULL
-- referencing resource/drop_resource) can't back ON CONFLICT clauses and the
-- resource column is being deprecated anyway.
DROP INDEX CONCURRENTLY IF EXISTS principal_grants_org_principal_scope_selector_key;
DROP INDEX CONCURRENTLY IF EXISTS principal_grants_org_principal_scope_unrestricted_key;
CREATE UNIQUE INDEX CONCURRENTLY principal_grants_org_principal_scope_selector_key
ON principal_grants (organization_id, principal_urn, scope, selectors);

-- Recreate GIN index without partial WHERE clause.
DROP INDEX CONCURRENTLY IF EXISTS principal_grants_selectors_idx;
CREATE INDEX CONCURRENTLY principal_grants_selectors_idx
ON principal_grants USING GIN (selectors);

-- Update comments to reflect the deprecation and new semantics.
COMMENT ON TABLE principal_grants IS 'RBAC grants. Normalized: one row per (org, principal, scope). Selectors can further constrain applicability.';
COMMENT ON COLUMN principal_grants.drop_resource IS 'Deprecated. Formerly ''*'' = unrestricted. Nullable, scheduled for removal.';
COMMENT ON COLUMN principal_grants.selectors IS 'Optional JSON selector constraints refining when the grant applies. NULL means the grant has no selector constraints.';
