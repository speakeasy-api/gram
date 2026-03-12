-- Create "scopes" table
CREATE TABLE "scopes" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "slug" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "scopes_slug_key" UNIQUE ("slug")
);
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
  CONSTRAINT "principal_grants_resources_check" CHECK ((resources IS NULL) OR ((array_length(resources, 1) >= 1) AND (array_length(resources, 1) <= 200)))
);
-- Create index "principal_grants_resources_idx" to table: "principal_grants"
CREATE INDEX "principal_grants_resources_idx" ON "principal_grants" USING gin ("resources");
