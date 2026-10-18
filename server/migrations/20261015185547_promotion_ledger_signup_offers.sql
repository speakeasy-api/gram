-- Create "signup_offers" table
CREATE TABLE "signup_offers" (
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "name" text NOT NULL,
  "stripe_promotion_code_id" text NOT NULL,
  "signup_flow" text NOT NULL,
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "enabled" boolean NOT NULL DEFAULT false,
  PRIMARY KEY ("id")
);
-- Create index "signup_offers_stripe_promotion_code_id_uidx" to table: "signup_offers"
CREATE UNIQUE INDEX "signup_offers_stripe_promotion_code_id_uidx" ON "signup_offers" ("stripe_promotion_code_id");
-- Set comment to column: "created_at" on table: "signup_offers"
COMMENT ON COLUMN "signup_offers"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "signup_offers"
COMMENT ON COLUMN "signup_offers"."updated_at" IS '@access: confidential';
-- Set comment to column: "name" on table: "signup_offers"
COMMENT ON COLUMN "signup_offers"."name" IS '@access: opaque-confidential';
-- Set comment to column: "stripe_promotion_code_id" on table: "signup_offers"
COMMENT ON COLUMN "signup_offers"."stripe_promotion_code_id" IS '@access: restricted';
-- Set comment to column: "signup_flow" on table: "signup_offers"
COMMENT ON COLUMN "signup_offers"."signup_flow" IS '@access: confidential';
-- Set comment to column: "id" on table: "signup_offers"
COMMENT ON COLUMN "signup_offers"."id" IS '@access: confidential';
-- Set comment to column: "enabled" on table: "signup_offers"
COMMENT ON COLUMN "signup_offers"."enabled" IS '@access: confidential';
-- Create "organization_promotion_applications" table
CREATE TABLE "organization_promotion_applications" (
  "redeemed_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "organization_id" text NULL,
  "stripe_promotion_code_id" text NOT NULL,
  "stripe_subscription_id" text NULL,
  "id" uuid NOT NULL DEFAULT gen_random_uuid(),
  PRIMARY KEY ("id"),
  CONSTRAINT "organization_promotion_applications_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "organization_promotion_applications_redemption_pair" CHECK ((redeemed_at IS NULL) = (stripe_subscription_id IS NULL))
);
-- Create index "organization_promotion_applications_org_promo_uidx" to table: "organization_promotion_applications"
CREATE UNIQUE INDEX "organization_promotion_applications_org_promo_uidx" ON "organization_promotion_applications" ("organization_id", "stripe_promotion_code_id");
-- Create index "organization_promotion_applications_pending_org_uidx" to table: "organization_promotion_applications"
CREATE UNIQUE INDEX "organization_promotion_applications_pending_org_uidx" ON "organization_promotion_applications" ("organization_id") WHERE (redeemed_at IS NULL);
-- Set comment to column: "redeemed_at" on table: "organization_promotion_applications"
COMMENT ON COLUMN "organization_promotion_applications"."redeemed_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "organization_promotion_applications"
COMMENT ON COLUMN "organization_promotion_applications"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "organization_promotion_applications"
COMMENT ON COLUMN "organization_promotion_applications"."updated_at" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "organization_promotion_applications"
COMMENT ON COLUMN "organization_promotion_applications"."organization_id" IS '@access: confidential';
-- Set comment to column: "stripe_promotion_code_id" on table: "organization_promotion_applications"
COMMENT ON COLUMN "organization_promotion_applications"."stripe_promotion_code_id" IS '@access: restricted';
-- Set comment to column: "stripe_subscription_id" on table: "organization_promotion_applications"
COMMENT ON COLUMN "organization_promotion_applications"."stripe_subscription_id" IS '@access: restricted';
-- Set comment to column: "id" on table: "organization_promotion_applications"
COMMENT ON COLUMN "organization_promotion_applications"."id" IS '@access: confidential';
