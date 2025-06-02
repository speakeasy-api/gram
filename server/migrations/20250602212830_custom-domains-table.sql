-- Create "custom_domains" table
CREATE TABLE "custom_domains" ("id" uuid NOT NULL DEFAULT generate_uuidv7(), "project_id" uuid NOT NULL, "domain" text NOT NULL, "verified" boolean NOT NULL DEFAULT false, "ingress_name" text NULL, "cert_secret_name" text NULL, "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "deleted_at" timestamptz NULL, "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED, PRIMARY KEY ("id"), CONSTRAINT "custom_domains_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE);
-- Create index "custom_domains_domain_key" to table: "custom_domains"
CREATE UNIQUE INDEX "custom_domains_domain_key" ON "custom_domains" ("domain") WHERE (deleted IS FALSE);
-- Create index "custom_domains_project_id_key" to table: "custom_domains"
CREATE UNIQUE INDEX "custom_domains_project_id_key" ON "custom_domains" ("project_id") WHERE (deleted IS FALSE);
