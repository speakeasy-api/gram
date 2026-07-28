-- Create "billing_cycle_usage" table
CREATE TABLE "billing_cycle_usage" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "cycle_start" timestamptz NOT NULL,
  "cycle_end" timestamptz NOT NULL,
  "tum_tokens" bigint NOT NULL DEFAULT 0,
  "finalized_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "billing_cycle_usage_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "billing_cycle_usage_cycle_bounds_check" CHECK (cycle_end > cycle_start)
);
-- Create index "billing_cycle_usage_organization_id_cycle_start_key" to table: "billing_cycle_usage"
CREATE UNIQUE INDEX "billing_cycle_usage_organization_id_cycle_start_key" ON "billing_cycle_usage" ("organization_id", "cycle_start");
