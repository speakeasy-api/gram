-- atlas:txmode none

-- Create index "api_keys_organization_id_created_at_id_idx" to table: "api_keys"
CREATE INDEX CONCURRENTLY "api_keys_organization_id_created_at_id_idx" ON "api_keys" ("organization_id", "created_at" DESC, "id" DESC) WHERE ((deleted IS FALSE) AND (subject_urn IS NULL));
