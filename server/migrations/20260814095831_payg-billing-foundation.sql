-- atlas:txmode none

-- Modify "billing_metadata" table
ALTER TABLE "billing_metadata" ADD COLUMN "stripe_customer_id" text NULL, ADD COLUMN "stripe_subscription_id" text NULL;
-- Create index "billing_metadata_stripe_customer_id_key" to table: "billing_metadata"
CREATE UNIQUE INDEX CONCURRENTLY "billing_metadata_stripe_customer_id_key" ON "billing_metadata" ("stripe_customer_id") WHERE (stripe_customer_id IS NOT NULL);
-- Create "openrouter_spend_daily" table
CREATE TABLE "openrouter_spend_daily" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "key_type" text NOT NULL DEFAULT 'chat',
  "day" date NOT NULL,
  "spend_usd" numeric(14,6) NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "openrouter_spend_daily_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "openrouter_spend_daily_spend_usd_check" CHECK (spend_usd >= (0)::numeric)
);
-- Create index "openrouter_spend_daily_organization_id_key_type_day_key" to table: "openrouter_spend_daily"
CREATE UNIQUE INDEX "openrouter_spend_daily_organization_id_key_type_day_key" ON "openrouter_spend_daily" ("organization_id", "key_type", "day");
-- Create "stripe_meter_reports" table
CREATE TABLE "stripe_meter_reports" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "cycle_start" timestamptz NOT NULL,
  "seq" integer NOT NULL,
  "delta_tokens" bigint NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "stripe_meter_reports_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "stripe_meter_reports_delta_tokens_check" CHECK (delta_tokens >= 0)
);
-- Create index "stripe_meter_reports_organization_id_cycle_start_seq_key" to table: "stripe_meter_reports"
CREATE UNIQUE INDEX "stripe_meter_reports_organization_id_cycle_start_seq_key" ON "stripe_meter_reports" ("organization_id", "cycle_start", "seq");
