-- Create "principal_grants" table
CREATE TABLE "principal_grants" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "principal_type" text NOT NULL,
  "principal_id" text NOT NULL,
  "scope_slug" text NOT NULL,
  "resource" text NOT NULL DEFAULT '*',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "principal_grants_organization_id_principal_type_principal_id_sc" UNIQUE ("organization_id", "principal_type", "principal_id", "scope_slug", "resource"),
  CONSTRAINT "principal_grants_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "principal_grants_principal_type_check" CHECK (principal_type = ANY (ARRAY['user'::text, 'role'::text]))
);
-- Set comment to table: "principal_grants"
COMMENT ON TABLE "principal_grants" IS 'RBAC grants. Normalized: one row per (org, principal, scope, resource). Resource=''*'' means unrestricted.';
-- Set comment to column: "organization_id" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."organization_id" IS 'The organization this grant belongs to. Grants are always org-scoped.';
-- Set comment to column: "principal_type" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."principal_type" IS 'Discriminator: ''user'' for a direct user grant, ''role'' for a WorkOS role grant.';
-- Set comment to column: "principal_id" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."principal_id" IS 'The identifier of the principal: a WorkOS user ID when principal_type=''user'', or a WorkOS role slug when principal_type=''role''.';
-- Set comment to column: "scope_slug" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."scope_slug" IS 'The scope being granted, e.g. "build:read". Validated in application code, not via FK.';
-- Set comment to column: "resource" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."resource" IS '''*'' = unrestricted (scope applies to all resources in the org). Any other value = a specific resource ID this scope is granted on.';
