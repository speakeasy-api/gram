-- atlas:txmode none

-- Add new unique constraints keyed on selectors instead of the old resource column.
-- Two partial indexes: one for rows with selectors, one for unrestricted (NULL) rows.

-- Rows WITH selectors: unique on full (org, principal, scope, selectors) tuple.
CREATE UNIQUE INDEX CONCURRENTLY principal_grants_org_principal_scope_selector_key
ON principal_grants (organization_id, principal_urn, scope, selectors)
WHERE selectors IS NOT NULL;

-- Rows WITHOUT selectors (unrestricted): unique on (org, principal, scope, resource).
-- Includes resource for backward compatibility — current code inserts multiple rows
-- per scope with different resource values. A later migration will drop resource
-- once all grants use selectors instead.
CREATE UNIQUE INDEX CONCURRENTLY principal_grants_org_principal_scope_unrestricted_key
ON principal_grants (organization_id, principal_urn, scope, resource)
WHERE selectors IS NULL;
