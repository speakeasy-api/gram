-- atlas:txmode none

-- Drop index "otel_forwarding_configs_org_key" from table: "otel_forwarding_configs"
DROP INDEX CONCURRENTLY "otel_forwarding_configs_org_key";
-- Drop index "otel_forwarding_configs_org_project_key" from table: "otel_forwarding_configs"
DROP INDEX CONCURRENTLY "otel_forwarding_configs_org_project_key";
-- Modify "otel_forwarding_configs" table
ALTER TABLE "otel_forwarding_configs" ADD COLUMN "name" text NOT NULL;
-- Create index "otel_forwarding_configs_org_enabled_idx" to table: "otel_forwarding_configs"
CREATE INDEX CONCURRENTLY "otel_forwarding_configs_org_enabled_idx" ON "otel_forwarding_configs" ("organization_id") WHERE ((enabled IS TRUE) AND (deleted IS FALSE));
-- Create index "otel_forwarding_configs_org_name_key" to table: "otel_forwarding_configs"
CREATE UNIQUE INDEX CONCURRENTLY "otel_forwarding_configs_org_name_key" ON "otel_forwarding_configs" ("organization_id", "name") WHERE ((project_id IS NULL) AND (deleted IS FALSE));
-- Create index "otel_forwarding_configs_org_project_name_key" to table: "otel_forwarding_configs"
CREATE UNIQUE INDEX CONCURRENTLY "otel_forwarding_configs_org_project_name_key" ON "otel_forwarding_configs" ("organization_id", "project_id", "name") WHERE ((project_id IS NOT NULL) AND (deleted IS FALSE));
-- Drop index "otel_forwarding_deliveries_pending_idx" from table: "otel_forwarding_deliveries"
DROP INDEX CONCURRENTLY "otel_forwarding_deliveries_pending_idx";
-- Modify "otel_forwarding_deliveries" table
ALTER TABLE "otel_forwarding_deliveries" DROP CONSTRAINT "otel_forwarding_deliveries_pkey", ADD COLUMN "destination_id" uuid NOT NULL, ADD PRIMARY KEY ("outbox_id", "destination_id"), ADD CONSTRAINT "otel_forwarding_deliveries_destination_id_fkey" FOREIGN KEY ("destination_id") REFERENCES "otel_forwarding_configs" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "otel_forwarding_deliveries_pending_idx" to table: "otel_forwarding_deliveries"
CREATE INDEX CONCURRENTLY "otel_forwarding_deliveries_pending_idx" ON "otel_forwarding_deliveries" ("outbox_id", "destination_id") WHERE ((processed_at IS NULL) AND (dead_lettered IS FALSE));
