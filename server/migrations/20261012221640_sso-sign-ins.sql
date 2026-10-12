-- atlas:txmode none

-- Create "sso_sign_ins" table
CREATE TABLE "sso_sign_ins" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "user_id" text NOT NULL,
  "workos_session_id" text NOT NULL,
  "authentication_method" text NOT NULL,
  "authenticated_at" timestamptz NOT NULL,
  "revoked_at" timestamptz NULL,
  "revoke_reason" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "sso_sign_ins_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "sso_sign_ins_organization_id_id_key" to table: "sso_sign_ins"
CREATE UNIQUE INDEX "sso_sign_ins_organization_id_id_key" ON "sso_sign_ins" ("organization_id", "id");
-- Create index "sso_sign_ins_organization_id_user_id_idx" to table: "sso_sign_ins"
CREATE INDEX "sso_sign_ins_organization_id_user_id_idx" ON "sso_sign_ins" ("organization_id", "user_id");
-- Create index "sso_sign_ins_organization_id_workos_session_id_key" to table: "sso_sign_ins"
CREATE UNIQUE INDEX "sso_sign_ins_organization_id_workos_session_id_key" ON "sso_sign_ins" ("organization_id", "workos_session_id");
-- Modify "user_sessions" table
ALTER TABLE "user_sessions" ADD COLUMN "sso_sign_in_id" uuid NULL, ADD CONSTRAINT "user_sessions_sso_sign_in_tenant_fkey" FOREIGN KEY ("organization_id", "sso_sign_in_id") REFERENCES "sso_sign_ins" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION NOT VALID;
ALTER TABLE "user_sessions" VALIDATE CONSTRAINT "user_sessions_sso_sign_in_tenant_fkey";
-- Create index "user_sessions_sso_sign_in_id_idx" to table: "user_sessions"
CREATE INDEX CONCURRENTLY "user_sessions_sso_sign_in_id_idx" ON "user_sessions" ("organization_id", "sso_sign_in_id") WHERE (sso_sign_in_id IS NOT NULL);
