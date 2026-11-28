-- Create "trial_payment_methods" table
CREATE TABLE "trial_payment_methods" (
  "organization_id" text NOT NULL,
  "stripe_customer_id" text NOT NULL,
  "stripe_payment_method_id" text NOT NULL,
  "card_fingerprint" text NOT NULL,
  "card_funding" text NOT NULL,
  "stripe_checkout_session_id" text NOT NULL,
  "rejection_reason" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("organization_id"),
  CONSTRAINT "trial_payment_methods_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "trial_payment_methods_card_fingerprint_idx" to table: "trial_payment_methods"
CREATE INDEX "trial_payment_methods_card_fingerprint_idx" ON "trial_payment_methods" ("card_fingerprint");
-- Set comment to column: "organization_id" on table: "trial_payment_methods"
COMMENT ON COLUMN "trial_payment_methods"."organization_id" IS '@access: confidential';
-- Set comment to column: "stripe_customer_id" on table: "trial_payment_methods"
COMMENT ON COLUMN "trial_payment_methods"."stripe_customer_id" IS '@access: restricted';
-- Set comment to column: "stripe_payment_method_id" on table: "trial_payment_methods"
COMMENT ON COLUMN "trial_payment_methods"."stripe_payment_method_id" IS '@access: restricted';
-- Set comment to column: "card_fingerprint" on table: "trial_payment_methods"
COMMENT ON COLUMN "trial_payment_methods"."card_fingerprint" IS '@access: restricted';
-- Set comment to column: "card_funding" on table: "trial_payment_methods"
COMMENT ON COLUMN "trial_payment_methods"."card_funding" IS '@access: confidential';
-- Set comment to column: "stripe_checkout_session_id" on table: "trial_payment_methods"
COMMENT ON COLUMN "trial_payment_methods"."stripe_checkout_session_id" IS '@access: restricted';
-- Set comment to column: "rejection_reason" on table: "trial_payment_methods"
COMMENT ON COLUMN "trial_payment_methods"."rejection_reason" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "trial_payment_methods"
COMMENT ON COLUMN "trial_payment_methods"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "trial_payment_methods"
COMMENT ON COLUMN "trial_payment_methods"."updated_at" IS '@access: confidential';
