-- Create "workload_identity_admissions" table
CREATE TABLE "workload_identity_admissions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "workload_issuer_id" uuid NOT NULL,
  "subject" text NOT NULL,
  "name" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "workload_identity_admissions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "workload_identity_admissions_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "workload_identity_admissions_workload_issuer_fkey" FOREIGN KEY ("organization_id", "workload_issuer_id") REFERENCES "workload_issuers" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "workload_identity_admissions_name_check" CHECK ((name IS NULL) OR (name <> ''::text)),
  CONSTRAINT "workload_identity_admissions_subject_check" CHECK (subject <> ''::text)
);
-- Create index "workload_identity_admissions_lookup_idx" to table: "workload_identity_admissions"
CREATE INDEX "workload_identity_admissions_lookup_idx" ON "workload_identity_admissions" ("organization_id", "workload_issuer_id", "subject") WHERE (deleted IS FALSE);
-- Create index "workload_identity_admissions_organization_key" to table: "workload_identity_admissions"
CREATE UNIQUE INDEX "workload_identity_admissions_organization_key" ON "workload_identity_admissions" ("organization_id", "workload_issuer_id", "subject") WHERE ((deleted IS FALSE) AND (project_id IS NULL));
-- Create index "workload_identity_admissions_project_key" to table: "workload_identity_admissions"
CREATE UNIQUE INDEX "workload_identity_admissions_project_key" ON "workload_identity_admissions" ("project_id", "workload_issuer_id", "subject") WHERE (deleted IS FALSE);
-- Create index "workload_identity_admissions_workload_issuer_id_idx" to table: "workload_identity_admissions"
CREATE INDEX "workload_identity_admissions_workload_issuer_id_idx" ON "workload_identity_admissions" ("workload_issuer_id") WHERE (deleted IS FALSE);
