-- atlas:txmode none

-- Create index "json_web_key_sets_external_key_tenant_idx" to table: "json_web_key_sets"
CREATE INDEX CONCURRENTLY "json_web_key_sets_external_key_tenant_idx" ON "json_web_key_sets" ("organization_id", "external_key_id");
-- Create index "json_web_keys_external_key_tenant_idx" to table: "json_web_keys"
CREATE INDEX CONCURRENTLY "json_web_keys_external_key_tenant_idx" ON "json_web_keys" ("organization_id", "external_key_id");
