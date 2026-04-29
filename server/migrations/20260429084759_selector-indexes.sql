-- atlas:txmode none

-- Replace the old CHECK with the tighter version. NOT VALID avoids ACCESS
-- EXCLUSIVE during the scan; VALIDATE downgrades to SHARE UPDATE EXCLUSIVE.
ALTER TABLE principal_grants DROP CONSTRAINT IF EXISTS principal_grants_selectors_check;
ALTER TABLE principal_grants ADD CONSTRAINT principal_grants_selectors_check CHECK (jsonb_typeof(selectors) = 'object' AND selectors != '{}') NOT VALID;
ALTER TABLE principal_grants VALIDATE CONSTRAINT principal_grants_selectors_check;

-- Add a NOT NULL CHECK so Postgres can skip a full-table scan on SET NOT NULL.
ALTER TABLE principal_grants ADD CONSTRAINT principal_grants_selectors_not_null CHECK (selectors IS NOT NULL) NOT VALID;
ALTER TABLE principal_grants VALIDATE CONSTRAINT principal_grants_selectors_not_null;
ALTER TABLE principal_grants ALTER COLUMN selectors SET NOT NULL;
ALTER TABLE principal_grants DROP CONSTRAINT principal_grants_selectors_not_null;

COMMENT ON COLUMN principal_grants.selectors IS 'JSON selector constraints attached to a grant. Must be a non-empty JSONB object. Wildcard/unrestricted grants use {"resource_kind":"*","resource_id":"*"}.';
