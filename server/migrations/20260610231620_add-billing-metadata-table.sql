-- Create "billing_metadata" table
CREATE TABLE "billing_metadata" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "tum_monthly_token_limit" bigint NULL,
  "alert_email" text NULL,
  "billing_cycle_anchor_day" integer NOT NULL DEFAULT 1,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "billing_metadata_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "billing_metadata_alert_email_check" CHECK ((alert_email IS NULL) OR (alert_email <> ''::text)),
  CONSTRAINT "billing_metadata_billing_cycle_anchor_day_check" CHECK ((billing_cycle_anchor_day >= 1) AND (billing_cycle_anchor_day <= 31))
);
-- Create index "billing_metadata_organization_id_key" to table: "billing_metadata"
CREATE UNIQUE INDEX "billing_metadata_organization_id_key" ON "billing_metadata" ("organization_id");
