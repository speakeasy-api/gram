-- Modify "billing_cycle_usage" table
ALTER TABLE "billing_cycle_usage" DROP CONSTRAINT "billing_cycle_usage_billed_frozen_at_check", DROP CONSTRAINT "billing_cycle_usage_billed_tum_tokens_check", DROP COLUMN "billed_tum_tokens", DROP COLUMN "billed_frozen_at";
-- Modify "stripe_invoice_allocations" table
ALTER TABLE "stripe_invoice_allocations" DROP CONSTRAINT "stripe_invoice_allocations_source_period_bounds_check", DROP COLUMN "source_period_start", DROP COLUMN "source_period_end", DROP COLUMN "delta_tokens", DROP COLUMN "original_tum_unit_price_usd";
-- Drop "stripe_meter_reports" table
DROP TABLE "stripe_meter_reports";
