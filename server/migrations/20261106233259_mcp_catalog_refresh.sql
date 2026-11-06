-- Create "mcp_catalog_refresh_plans" table
CREATE TABLE "mcp_catalog_refresh_plans" (
  "id" uuid NOT NULL,
  "source_identity" text NOT NULL,
  "owner" text NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "data" jsonb NOT NULL,
  "receipt" jsonb NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_catalog_refresh_plans_data_check" CHECK (octet_length((data)::text) <= 67108864),
  CONSTRAINT "mcp_catalog_refresh_plans_receipt_check" CHECK (octet_length((receipt)::text) <= 67108864)
);
-- Set comment to column: "id" on table: "mcp_catalog_refresh_plans"
COMMENT ON COLUMN "mcp_catalog_refresh_plans"."id" IS '@access: confidential';
-- Set comment to column: "source_identity" on table: "mcp_catalog_refresh_plans"
COMMENT ON COLUMN "mcp_catalog_refresh_plans"."source_identity" IS 'Catalog source identity.
@access: restricted';
-- Set comment to column: "owner" on table: "mcp_catalog_refresh_plans"
COMMENT ON COLUMN "mcp_catalog_refresh_plans"."owner" IS 'Plan owner identity.
@access: confidential-pii';
-- Set comment to column: "expires_at" on table: "mcp_catalog_refresh_plans"
COMMENT ON COLUMN "mcp_catalog_refresh_plans"."expires_at" IS '@access: confidential';
-- Set comment to column: "data" on table: "mcp_catalog_refresh_plans"
COMMENT ON COLUMN "mcp_catalog_refresh_plans"."data" IS 'Catalog refresh plan.
@access: opaque-restricted';
-- Set comment to column: "receipt" on table: "mcp_catalog_refresh_plans"
COMMENT ON COLUMN "mcp_catalog_refresh_plans"."receipt" IS 'Completed refresh receipt.
@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "mcp_catalog_refresh_plans"
COMMENT ON COLUMN "mcp_catalog_refresh_plans"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_catalog_refresh_plans"
COMMENT ON COLUMN "mcp_catalog_refresh_plans"."updated_at" IS '@access: confidential';
-- Create "mcp_catalog_refresh_state" table
CREATE TABLE "mcp_catalog_refresh_state" (
  "source_identity" text NOT NULL,
  "state" jsonb NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("source_identity"),
  CONSTRAINT "mcp_catalog_refresh_state_state_check" CHECK (octet_length((state)::text) <= 67108864)
);
-- Set comment to column: "source_identity" on table: "mcp_catalog_refresh_state"
COMMENT ON COLUMN "mcp_catalog_refresh_state"."source_identity" IS 'Catalog source identity.
@access: restricted';
-- Set comment to column: "state" on table: "mcp_catalog_refresh_state"
COMMENT ON COLUMN "mcp_catalog_refresh_state"."state" IS 'Catalog refresh state.
@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "mcp_catalog_refresh_state"
COMMENT ON COLUMN "mcp_catalog_refresh_state"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_catalog_refresh_state"
COMMENT ON COLUMN "mcp_catalog_refresh_state"."updated_at" IS '@access: confidential';
