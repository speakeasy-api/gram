-- Create "scopes" table
CREATE TABLE "scopes" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "slug" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "scopes_slug_key" UNIQUE ("slug")
);
-- Set comment to table: "scopes"
COMMENT ON TABLE "scopes" IS 'RBAC scope vocabulary. Reference data seeded at app startup.';
-- Set comment to column: "slug" on table: "scopes"
COMMENT ON COLUMN "scopes"."slug" IS 'Unique human-readable identifier, e.g. "project:read", "build:write". Used as the FK target from principal_grants so grant rows are self-describing.';
-- Create "principal_grants" table
CREATE TABLE "principal_grants" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "principal_type" text NOT NULL,
  "principal_id" text NOT NULL,
  "scope_slug" text NOT NULL,
  "resources" text[] NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "principal_grants_organization_id_principal_type_principal_id_sc" UNIQUE ("organization_id", "principal_type", "principal_id", "scope_slug"),
  CONSTRAINT "principal_grants_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "principal_grants_scope_slug_fkey" FOREIGN KEY ("scope_slug") REFERENCES "scopes" ("slug") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "principal_grants_principal_type_check" CHECK (principal_type = ANY (ARRAY['user'::text, 'role'::text])),
  CONSTRAINT "principal_grants_resources_check" CHECK ((resources IS NULL) OR ((cardinality(resources) >= 1) AND (cardinality(resources) <= 200)))
);
-- Create index "principal_grants_resources_idx" to table: "principal_grants"
CREATE INDEX "principal_grants_resources_idx" ON "principal_grants" USING gin ("resources");
-- Set comment to table: "principal_grants"
COMMENT ON TABLE "principal_grants" IS 'RBAC principal grants. One row per (org, principal, scope). The UNIQUE constraint guarantees at most one row per combination, so NULL resources (unrestricted) and array resources (allowlist) are mutually exclusive by construction.';
-- Set comment to column: "organization_id" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."organization_id" IS 'The organization this grant belongs to. Grants are always org-scoped.';
-- Set comment to column: "principal_type" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."principal_type" IS 'Discriminator: ''user'' for a direct user grant, ''role'' for a WorkOS role grant.';
-- Set comment to column: "principal_id" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."principal_id" IS 'The identifier of the principal: a WorkOS user ID when principal_type=''user'', or a WorkOS role slug when principal_type=''role''.';
-- Set comment to column: "scope_slug" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."scope_slug" IS 'The scope being granted, e.g. "project:read". References scopes(slug).';
-- Set comment to column: "resources" on table: "principal_grants"
COMMENT ON COLUMN "principal_grants"."resources" IS 'NULL = unrestricted (scope applies to all resources in the org). Non-empty array = allowlist of resource IDs this scope is restricted to. Empty arrays are rejected by the CHECK constraint.';
-- Set comment to index: "principal_grants_resources_idx" on table: "principal_grants"
COMMENT ON INDEX "principal_grants_resources_idx" IS 'Supports efficient @> (array containment) queries used by access.Filter() to find grants covering a specific resource ID.';
