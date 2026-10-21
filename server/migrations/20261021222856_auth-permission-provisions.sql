-- Create "auth_permission_provisions" table
CREATE TABLE "auth_permission_provisions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "principal_urn" text NOT NULL,
  "scope" text NOT NULL,
  "selectors" jsonb NOT NULL,
  "kind" text NOT NULL,
  "taken_over_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "auth_permission_provisions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "auth_permission_provisions_kind_check" CHECK (kind <> ''::text),
  CONSTRAINT "auth_permission_provisions_principal_urn_check" CHECK (principal_urn <> ''::text),
  CONSTRAINT "auth_permission_provisions_scope_check" CHECK (scope <> ''::text),
  CONSTRAINT "auth_permission_provisions_selectors_check" CHECK ((jsonb_typeof(selectors) = 'object'::text) AND (selectors <> '{}'::jsonb))
);
-- Create index "auth_permission_provisions_org_principal_scope_selector_key" to table: "auth_permission_provisions"
CREATE UNIQUE INDEX "auth_permission_provisions_org_principal_scope_selector_key" ON "auth_permission_provisions" ("organization_id", "principal_urn", "scope", "selectors");
-- Set comment to table: "auth_permission_provisions"
COMMENT ON TABLE "auth_permission_provisions" IS 'Default auth permission grants the backfill provisioned, one row per grant, kept after the grant itself is deleted.';
-- Set comment to column: "id" on table: "auth_permission_provisions"
COMMENT ON COLUMN "auth_permission_provisions"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "auth_permission_provisions"
COMMENT ON COLUMN "auth_permission_provisions"."organization_id" IS 'The organization the grant belongs to.
@access: confidential';
-- Set comment to column: "principal_urn" on table: "auth_permission_provisions"
COMMENT ON COLUMN "auth_permission_provisions"."principal_urn" IS 'The principal the grant was written to, as in principal_grants.principal_urn.
@access: confidential-pii';
-- Set comment to column: "scope" on table: "auth_permission_provisions"
COMMENT ON COLUMN "auth_permission_provisions"."scope" IS 'The granted scope, mcp:auth or mcp:connect.
@access: confidential';
-- Set comment to column: "selectors" on table: "auth_permission_provisions"
COMMENT ON COLUMN "auth_permission_provisions"."selectors" IS 'The grant''s selector, as in principal_grants.selectors.
@access: opaque-restricted';
-- Set comment to column: "kind" on table: "auth_permission_provisions"
COMMENT ON COLUMN "auth_permission_provisions"."kind" IS 'Which default the grant belongs to: personal_default, shared_client or api_key. Validated in application code.
@access: confidential';
-- Set comment to column: "taken_over_at" on table: "auth_permission_provisions"
COMMENT ON COLUMN "auth_permission_provisions"."taken_over_at" IS 'When an admin wrote the same grant, making it theirs: a rollback leaves it. NULL while it is still provisioned.
@access: confidential';
-- Set comment to column: "created_at" on table: "auth_permission_provisions"
COMMENT ON COLUMN "auth_permission_provisions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "auth_permission_provisions"
COMMENT ON COLUMN "auth_permission_provisions"."updated_at" IS '@access: confidential';
