-- Create "workload_issuers" table
CREATE TABLE "workload_issuers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "name" text NOT NULL,
  "tags" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "issuer" text NOT NULL,
  "jwks_uri" text NOT NULL,
  "metadata" jsonb NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "workload_issuers_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "workload_issuers_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "workload_issuers_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 100)),
  CONSTRAINT "workload_issuers_tags_check" CHECK (array_length(tags, 1) <= 40)
);
-- Create index "workload_issuers_issuer_idx" to table: "workload_issuers"
CREATE INDEX "workload_issuers_issuer_idx" ON "workload_issuers" ("issuer") WHERE (deleted IS FALSE);
-- Create index "workload_issuers_organization_id_id_key" to table: "workload_issuers"
CREATE UNIQUE INDEX "workload_issuers_organization_id_id_key" ON "workload_issuers" ("organization_id", "id");
-- Create index "workload_issuers_organization_name_key" to table: "workload_issuers"
CREATE UNIQUE INDEX "workload_issuers_organization_name_key" ON "workload_issuers" ("organization_id", "name") WHERE ((deleted IS FALSE) AND (project_id IS NULL));
-- Create index "workload_issuers_project_name_key" to table: "workload_issuers"
CREATE UNIQUE INDEX "workload_issuers_project_name_key" ON "workload_issuers" ("project_id", "name") WHERE (deleted IS FALSE);
-- Create index "workload_issuers_tags_gin" to table: "workload_issuers"
CREATE INDEX "workload_issuers_tags_gin" ON "workload_issuers" USING GIN ("tags") WHERE (deleted IS FALSE);
