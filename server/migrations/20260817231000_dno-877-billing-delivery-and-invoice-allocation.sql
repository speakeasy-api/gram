-- atlas:txmode none

-- Modify "billing_metadata" table
ALTER TABLE "billing_metadata" ADD COLUMN "stripe_billing_cycle_anchor" timestamptz NULL, ADD COLUMN "stripe_checkout_idempotency_key" text NULL, ADD COLUMN "stripe_checkout_billing_cycle_anchor" timestamptz NULL, ADD COLUMN "stripe_checkout_trial_end" timestamptz NULL, ADD COLUMN "stripe_checkout_expires_at" timestamptz NULL, ADD COLUMN "stripe_checkout_session_id" text NULL;
-- Create index "billing_metadata_stripe_checkout_session_id_key" to table: "billing_metadata"
CREATE UNIQUE INDEX CONCURRENTLY "billing_metadata_stripe_checkout_session_id_key" ON "billing_metadata" ("stripe_checkout_session_id") WHERE (stripe_checkout_session_id IS NOT NULL);
-- Modify "billing_cycle_usage" table
ALTER TABLE "billing_cycle_usage" DROP CONSTRAINT "billing_cycle_usage_organization_id_fkey", ADD CONSTRAINT "billing_cycle_usage_billed_frozen_at_check" CHECK ((billed_frozen_at IS NULL) OR (billed_tum_tokens IS NOT NULL)), ADD CONSTRAINT "billing_cycle_usage_billed_tum_tokens_check" CHECK ((billed_tum_tokens IS NULL) OR (billed_tum_tokens >= 0)), ALTER COLUMN "organization_id" DROP NOT NULL, ADD COLUMN "billed_tum_tokens" bigint NULL, ADD COLUMN "billed_frozen_at" timestamptz NULL, ADD CONSTRAINT "billing_cycle_usage_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "billing_cycle_usage_organization_id_id_key" to table: "billing_cycle_usage"
CREATE UNIQUE INDEX CONCURRENTLY "billing_cycle_usage_organization_id_id_key" ON "billing_cycle_usage" ("organization_id", "id");
-- Create "stripe_invoices" table
CREATE TABLE "stripe_invoices" (
  "stripe_invoice_id" text NOT NULL,
  "organization_id" text NULL,
  "stripe_customer_id" text NOT NULL,
  "stripe_subscription_id" text NOT NULL,
  "service_period_start" timestamptz NOT NULL,
  "service_period_end" timestamptz NOT NULL,
  "invoice_state" text NOT NULL,
  "finalized_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("stripe_invoice_id"),
  CONSTRAINT "stripe_invoices_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "stripe_invoices_service_period_bounds_check" CHECK (service_period_end > service_period_start)
);
-- Create index "stripe_invoices_organization_id_service_period_start_idx" to table: "stripe_invoices"
CREATE INDEX "stripe_invoices_organization_id_service_period_start_idx" ON "stripe_invoices" ("organization_id", "service_period_start") WHERE (organization_id IS NOT NULL);
-- Create index "stripe_invoices_organization_id_stripe_invoice_id_key" to table: "stripe_invoices"
CREATE UNIQUE INDEX "stripe_invoices_organization_id_stripe_invoice_id_key" ON "stripe_invoices" ("organization_id", "stripe_invoice_id");
-- Create "stripe_invoice_allocations" table
CREATE TABLE "stripe_invoice_allocations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NULL,
  "source_kind" text NOT NULL,
  "source_key" text NOT NULL,
  "seq" integer NOT NULL,
  "source_day" date NULL,
  "source_period_start" timestamptz NULL,
  "source_period_end" timestamptz NULL,
  "source_snapshot_usd" numeric(14,6) NULL,
  "delta_tokens" bigint NULL,
  "original_tum_unit_price_usd" numeric(14,12) NULL,
  "amount_usd" numeric(14,6) NULL,
  "original_invoice_id" text NULL,
  "destination_invoice_id" text NULL,
  "stripe_invoice_item_id" text NULL,
  "stripe_credit_note_id" text NULL,
  "idempotency_key" text NOT NULL,
  "delivery_state" text NOT NULL DEFAULT 'pending',
  "first_attempted_at" timestamptz NULL,
  "last_attempted_at" timestamptz NULL,
  "confirmed_at" timestamptz NULL,
  "ambiguous_at" timestamptz NULL,
  "reconciled_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "stripe_invoice_allocations_org_destination_invoice_fkey" FOREIGN KEY ("organization_id", "destination_invoice_id") REFERENCES "stripe_invoices" ("organization_id", "stripe_invoice_id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "stripe_invoice_allocations_org_original_invoice_fkey" FOREIGN KEY ("organization_id", "original_invoice_id") REFERENCES "stripe_invoices" ("organization_id", "stripe_invoice_id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "stripe_invoice_allocations_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "stripe_invoice_allocations_source_period_bounds_check" CHECK (((source_day IS NOT NULL) AND (source_period_start IS NULL) AND (source_period_end IS NULL)) OR ((source_day IS NULL) AND (source_period_start IS NOT NULL) AND (source_period_end IS NOT NULL) AND (source_period_end > source_period_start))),
  CONSTRAINT "stripe_invoice_allocations_source_snapshot_usd_check" CHECK ((source_snapshot_usd IS NULL) OR (source_snapshot_usd >= (0)::numeric))
);
-- Create index "stripe_invoice_allocations_destination_invoice_id_idx" to table: "stripe_invoice_allocations"
CREATE INDEX "stripe_invoice_allocations_destination_invoice_id_idx" ON "stripe_invoice_allocations" ("destination_invoice_id") WHERE (destination_invoice_id IS NOT NULL);
-- Create index "stripe_invoice_allocations_idempotency_key_key" to table: "stripe_invoice_allocations"
CREATE UNIQUE INDEX "stripe_invoice_allocations_idempotency_key_key" ON "stripe_invoice_allocations" ("idempotency_key");
-- Create index "stripe_invoice_allocations_org_source_seq_key" to table: "stripe_invoice_allocations"
CREATE UNIQUE INDEX "stripe_invoice_allocations_org_source_seq_key" ON "stripe_invoice_allocations" ("organization_id", "source_kind", "source_key", "seq");
-- Create index "stripe_invoice_allocations_original_invoice_id_idx" to table: "stripe_invoice_allocations"
CREATE INDEX "stripe_invoice_allocations_original_invoice_id_idx" ON "stripe_invoice_allocations" ("original_invoice_id") WHERE (original_invoice_id IS NOT NULL);
-- Modify "stripe_meter_reports" table
ALTER TABLE "stripe_meter_reports" DROP CONSTRAINT "stripe_meter_reports_delta_tokens_check", DROP CONSTRAINT "stripe_meter_reports_organization_id_fkey", ADD CONSTRAINT "stripe_meter_reports_cycle_bounds_check" CHECK ((cycle_end IS NULL) OR (cycle_end > cycle_start)), ADD CONSTRAINT "stripe_meter_reports_event_timestamp_check" CHECK ((event_timestamp IS NULL) OR ((cycle_end IS NOT NULL) AND (event_timestamp >= cycle_start) AND (event_timestamp < cycle_end))), ALTER COLUMN "organization_id" DROP NOT NULL, ADD COLUMN "billing_cycle_usage_id" uuid NULL, ADD COLUMN "cycle_end" timestamptz NULL, ADD COLUMN "stripe_customer_id" text NULL, ADD COLUMN "stripe_meter_event_name" text NULL, ADD COLUMN "stripe_identifier" text NULL, ADD COLUMN "event_timestamp" timestamptz NULL, ADD COLUMN "delivery_state" text NOT NULL DEFAULT 'confirmed', ADD COLUMN "first_attempted_at" timestamptz NULL, ADD COLUMN "last_attempted_at" timestamptz NULL, ADD COLUMN "confirmed_at" timestamptz NULL, ADD COLUMN "ambiguous_at" timestamptz NULL, ADD COLUMN "reconciled_at" timestamptz NULL, ADD CONSTRAINT "stripe_meter_reports_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "stripe_meter_reports_org_billing_cycle_usage_fkey" FOREIGN KEY ("organization_id", "billing_cycle_usage_id") REFERENCES "billing_cycle_usage" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "stripe_meter_reports_billing_cycle_usage_id_idx" to table: "stripe_meter_reports"
CREATE INDEX CONCURRENTLY "stripe_meter_reports_billing_cycle_usage_id_idx" ON "stripe_meter_reports" ("billing_cycle_usage_id") WHERE (billing_cycle_usage_id IS NOT NULL);
-- Create index "stripe_meter_reports_org_cycle_bounds_seq_key" to table: "stripe_meter_reports"
CREATE UNIQUE INDEX CONCURRENTLY "stripe_meter_reports_org_cycle_bounds_seq_key" ON "stripe_meter_reports" ("organization_id", "cycle_start", "cycle_end", "seq");
-- Create index "stripe_meter_reports_stripe_identifier_key" to table: "stripe_meter_reports"
CREATE UNIQUE INDEX CONCURRENTLY "stripe_meter_reports_stripe_identifier_key" ON "stripe_meter_reports" ("stripe_identifier") WHERE (stripe_identifier IS NOT NULL);
