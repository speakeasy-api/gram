-- Create "stripe_webhook_receipts" table
CREATE TABLE "stripe_webhook_receipts" (
  "stripe_event_id" text NOT NULL,
  "organization_id" text NOT NULL,
  "event_type" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("stripe_event_id"),
  CONSTRAINT "stripe_webhook_receipts_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "stripe_webhook_receipts_organization_id_idx" to table: "stripe_webhook_receipts"
CREATE INDEX "stripe_webhook_receipts_organization_id_idx" ON "stripe_webhook_receipts" ("organization_id");
