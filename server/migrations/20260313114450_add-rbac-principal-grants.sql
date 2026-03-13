-- Create "principal_grants" table
CREATE TABLE "principal_grants" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "principal_urn" text NOT NULL,
  "principal_type" text NOT NULL GENERATED ALWAYS AS (split_part(principal_urn, ':'::text, 1)) STORED,
  "scope" text NOT NULL,
  "resource" text NOT NULL DEFAULT '*',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "principal_grants_org_principal_scope_resource_key" UNIQUE ("organization_id", "principal_urn", "scope", "resource"),
  CONSTRAINT "principal_grants_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to table: "principal_grants"
COMMENT ON TABLE "principal_grants" IS 'RBAC grants. Normalized: one row per (org, principal, scope, resource). Resource=''*'' means unrestricted.';
-- Set comment to column: "organization_id" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."organization_id" IS 'The organization this grant belongs to. Grants are always org-scoped.';
-- Set comment to column: "principal_urn" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."principal_urn" IS 'URN identifying the principal, e.g. "user:user_abc", "role:admin". Format is type:id.';
-- Set comment to column: "principal_type" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."principal_type" IS 'Derived from principal_urn. The type prefix, e.g. "user", "role".';
-- Set comment to column: "scope" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."scope" IS 'The scope being granted, e.g. "build:read". Validated in application code, not via FK.';
-- Set comment to column: "resource" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."resource" IS '''*'' = unrestricted (scope applies to all resources in the org). Any other value = a specific resource ID this scope is granted on.';
