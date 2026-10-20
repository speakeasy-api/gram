-- Create "remote_protected_resources" table
CREATE TABLE "remote_protected_resources" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "resource_identifier" text NOT NULL,
  "metadata_url" text NULL,
  "authorization_servers" text[] NULL,
  "scopes_supported" text[] NULL,
  "bearer_methods_supported" text[] NULL,
  "resource_name" text NULL,
  "resource_documentation" text NULL,
  "resource_policy_uri" text NULL,
  "resource_tos_uri" text NULL,
  "dpop_bound_access_tokens_required" boolean NULL,
  "dpop_signing_alg_values_supported" text[] NULL,
  "tls_client_certificate_bound_access_tokens" boolean NULL,
  "challenge_scopes" text[] NULL,
  "challenge_scopes_seen_at" timestamptz NULL,
  "metadata" jsonb NULL,
  "metadata_fetched_at" timestamptz NULL,
  "metadata_last_error" text NULL,
  "metadata_last_error_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "remote_protected_resources_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "remote_protected_resources_resource_identifier_check" CHECK (resource_identifier <> ''::text)
);
-- Create index "remote_protected_resources_project_id_id_key" to table: "remote_protected_resources"
CREATE UNIQUE INDEX "remote_protected_resources_project_id_id_key" ON "remote_protected_resources" ("project_id", "id");
-- Create index "remote_protected_resources_project_id_resource_identifier_key" to table: "remote_protected_resources"
CREATE UNIQUE INDEX "remote_protected_resources_project_id_resource_identifier_key" ON "remote_protected_resources" ("project_id", "resource_identifier") WHERE (deleted IS FALSE);
