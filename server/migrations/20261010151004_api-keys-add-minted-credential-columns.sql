-- atlas:txmode none

-- Modify "api_keys" table
ALTER TABLE "api_keys" ADD COLUMN "parent_api_key_id" uuid NULL, ADD COLUMN "device_id" text NULL;
-- Create index "api_keys_parent_api_key_id_device_id_idx" to table: "api_keys"
CREATE INDEX CONCURRENTLY "api_keys_parent_api_key_id_device_id_idx" ON "api_keys" ("parent_api_key_id", "device_id") WHERE ((parent_api_key_id IS NOT NULL) AND (deleted IS FALSE));
