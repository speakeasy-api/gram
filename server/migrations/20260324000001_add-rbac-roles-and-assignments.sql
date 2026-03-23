-- Create "roles" table
CREATE TABLE "roles" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "name" text NOT NULL,
  "description" text NOT NULL DEFAULT '',
  "is_system" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "roles_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "roles_description_check" CHECK (char_length(description) <= 500),
  CONSTRAINT "roles_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 100))
);
-- Create index "roles_organization_id_name_key" to table: "roles"
CREATE UNIQUE INDEX "roles_organization_id_name_key" ON "roles" ("organization_id", "name") WHERE (deleted IS FALSE);
-- Set comment to table: "roles"
COMMENT ON TABLE "roles" IS 'Named role definitions. Scopes are assigned via principal_grants with principal_urn = ''role:<role_id>''.';
-- Set comment to column: "is_system" on table: "roles"
COMMENT ON COLUMN "roles"."is_system" IS 'System roles (Admin, Member) are seeded on org creation and cannot be deleted or renamed.';
-- Create "role_assignments" table
CREATE TABLE "role_assignments" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "user_id" text NOT NULL,
  "role_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "role_assignments_organization_id_user_id_key" UNIQUE ("organization_id", "user_id"),
  CONSTRAINT "role_assignments_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "role_assignments_role_id_fkey" FOREIGN KEY ("role_id") REFERENCES "roles" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "role_assignments_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Set comment to table: "role_assignments"
COMMENT ON TABLE "role_assignments" IS 'Maps each user to exactly one role per organization. Used by the access resolver to look up role grants for a user.';
-- Set comment to column: "role_id" on table: "role_assignments"
COMMENT ON COLUMN "role_assignments"."role_id" IS 'The role assigned to this user. Grants for the role are in principal_grants with principal_urn = ''role:<role_id>''.';
