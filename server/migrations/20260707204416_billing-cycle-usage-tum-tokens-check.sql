-- Modify "billing_cycle_usage" table
ALTER TABLE "billing_cycle_usage" ADD CONSTRAINT "billing_cycle_usage_tum_tokens_check" CHECK (tum_tokens >= 0);
