-- atlas:txmode none

-- Create index "skill_observations_scoped_invalid_reconciliation_idx" to table: "skill_observations"
CREATE INDEX CONCURRENTLY "skill_observations_scoped_invalid_reconciliation_idx" ON "skill_observations" ("project_id", "seen_at", "id") WHERE ((reconcile_error_code = 'invalid_name'::text) AND (btrim(skill_name) ~ '^[^:]+:[[:space:]]*[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$'::text) AND (char_length(btrim("substring"(btrim(skill_name), '[^:]+$'::text))) <= 64));
