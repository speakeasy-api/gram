-- atlas:txmode none

-- Add new unique constraints keyed on selectors instead of the old resource column.
-- Two partial indexes: one for rows with selectors, one for unrestricted (NULL) rows.

-- Rows WITH selectors: unique on full (org, principal, scope, selectors) tuple.
CREATE UNIQUE INDEX CONCURRENTLY principal_grants_org_principal_scope_selector_key
ON principal_grants (organization_id, principal_urn, scope, selectors)
WHERE selectors IS NOT NULL;

-- Rows WITHOUT selectors (unrestricted): unique on (org, principal, scope).
CREATE UNIQUE INDEX CONCURRENTLY principal_grants_org_principal_scope_unrestricted_key
ON principal_grants (organization_id, principal_urn, scope)
WHERE selectors IS NULL;
