-- atlas:txmode none

-- Create index "user_session_clients_issuer_id_key" to table: "user_session_clients"
CREATE UNIQUE INDEX CONCURRENTLY "user_session_clients_issuer_id_key" ON "user_session_clients" ("user_session_issuer_id", "id");
-- Create "issuance_selection_profiles" table
CREATE TABLE "issuance_selection_profiles" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "user_session_issuer_id" uuid NOT NULL,
  "user_session_client_id" uuid NULL,
  "workload_issuer_id" uuid NULL,
  "workload_subject" text NULL,
  "name" text NOT NULL,
  "created_by_user_id" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "issuance_selection_profiles_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "issuance_selection_profiles_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "issuance_selection_profiles_user_session_client_fkey" FOREIGN KEY ("user_session_issuer_id", "user_session_client_id") REFERENCES "user_session_clients" ("user_session_issuer_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "issuance_selection_profiles_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "issuance_selection_profiles_workload_issuer_tenant_fkey" FOREIGN KEY ("organization_id", "workload_issuer_id") REFERENCES "workload_issuers" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "issuance_selection_profiles_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 100)),
  CONSTRAINT "issuance_selection_profiles_workload_subject_check" CHECK ((workload_subject IS NULL) OR (workload_subject <> ''::text))
);
-- Create index "issuance_selection_profiles_client_key" to table: "issuance_selection_profiles"
CREATE UNIQUE INDEX "issuance_selection_profiles_client_key" ON "issuance_selection_profiles" ("user_session_issuer_id", "user_session_client_id") WHERE ((deleted IS FALSE) AND (user_session_client_id IS NOT NULL));
-- Create index "issuance_selection_profiles_organization_id_idx" to table: "issuance_selection_profiles"
CREATE INDEX "issuance_selection_profiles_organization_id_idx" ON "issuance_selection_profiles" ("organization_id") WHERE (deleted IS FALSE);
-- Create index "issuance_selection_profiles_project_id_idx" to table: "issuance_selection_profiles"
CREATE INDEX "issuance_selection_profiles_project_id_idx" ON "issuance_selection_profiles" ("project_id");
-- Create index "issuance_selection_profiles_user_session_client_id_idx" to table: "issuance_selection_profiles"
CREATE INDEX "issuance_selection_profiles_user_session_client_id_idx" ON "issuance_selection_profiles" ("user_session_client_id");
-- Create index "issuance_selection_profiles_workload_issuer_id_idx" to table: "issuance_selection_profiles"
CREATE INDEX "issuance_selection_profiles_workload_issuer_id_idx" ON "issuance_selection_profiles" ("workload_issuer_id");
-- Create index "issuance_selection_profiles_workload_key" to table: "issuance_selection_profiles"
CREATE UNIQUE INDEX "issuance_selection_profiles_workload_key" ON "issuance_selection_profiles" ("user_session_issuer_id", "workload_issuer_id", "workload_subject") WHERE ((deleted IS FALSE) AND (workload_issuer_id IS NOT NULL));
-- Set comment to column: "id" on table: "issuance_selection_profiles"
COMMENT ON COLUMN "issuance_selection_profiles"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "issuance_selection_profiles"
COMMENT ON COLUMN "issuance_selection_profiles"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "issuance_selection_profiles"
COMMENT ON COLUMN "issuance_selection_profiles"."project_id" IS '@access: confidential';
-- Set comment to column: "user_session_issuer_id" on table: "issuance_selection_profiles"
COMMENT ON COLUMN "issuance_selection_profiles"."user_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "user_session_client_id" on table: "issuance_selection_profiles"
COMMENT ON COLUMN "issuance_selection_profiles"."user_session_client_id" IS '@access: confidential';
-- Set comment to column: "workload_issuer_id" on table: "issuance_selection_profiles"
COMMENT ON COLUMN "issuance_selection_profiles"."workload_issuer_id" IS '@access: confidential';
-- Set comment to column: "workload_subject" on table: "issuance_selection_profiles"
COMMENT ON COLUMN "issuance_selection_profiles"."workload_subject" IS '@access: restricted';
-- Set comment to column: "name" on table: "issuance_selection_profiles"
COMMENT ON COLUMN "issuance_selection_profiles"."name" IS '@access: opaque-confidential';
-- Set comment to column: "created_by_user_id" on table: "issuance_selection_profiles"
COMMENT ON COLUMN "issuance_selection_profiles"."created_by_user_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "issuance_selection_profiles"
COMMENT ON COLUMN "issuance_selection_profiles"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "issuance_selection_profiles"
COMMENT ON COLUMN "issuance_selection_profiles"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "issuance_selection_profiles"
COMMENT ON COLUMN "issuance_selection_profiles"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "issuance_selection_profiles"
COMMENT ON COLUMN "issuance_selection_profiles"."deleted" IS '@access: confidential';
