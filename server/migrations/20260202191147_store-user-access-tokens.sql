-- Create "external_oauth_client_registrations" table
CREATE TABLE "external_oauth_client_registrations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "oauth_server_issuer" text NOT NULL,
  "client_id" text NOT NULL,
  "client_secret_encrypted" text NULL,
  "client_id_issued_at" timestamptz NULL,
  "client_secret_expires_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "external_oauth_client_registrations_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "external_oauth_client_registrations_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "external_oauth_client_registrations_client_id_check" CHECK (client_id <> ''::text),
  CONSTRAINT "external_oauth_client_registrations_oauth_server_issuer_check" CHECK (oauth_server_issuer <> ''::text)
);
-- Create index "external_oauth_client_registrations_org_issuer_key" to table: "external_oauth_client_registrations"
CREATE UNIQUE INDEX "external_oauth_client_registrations_org_issuer_key" ON "external_oauth_client_registrations" ("organization_id", "oauth_server_issuer") WHERE (deleted IS FALSE);
-- Create "user_oauth_tokens" table
CREATE TABLE "user_oauth_tokens" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "user_id" text NOT NULL,
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "client_registration_id" uuid NOT NULL,
  "toolset_id" uuid NOT NULL,
  "oauth_server_issuer" text NOT NULL,
  "access_token_encrypted" text NOT NULL,
  "refresh_token_encrypted" text NULL,
  "token_type" text NULL,
  "expires_at" timestamptz NULL,
  "scopes" text[] NOT NULL,
  "provider_name" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "user_oauth_tokens_client_registration_id_fkey" FOREIGN KEY ("client_registration_id") REFERENCES "external_oauth_client_registrations" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_oauth_tokens_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_oauth_tokens_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_oauth_tokens_toolset_id_fkey" FOREIGN KEY ("toolset_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_oauth_tokens_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_oauth_tokens_oauth_server_issuer_check" CHECK ((oauth_server_issuer <> ''::text) AND (char_length(oauth_server_issuer) <= 500))
);
-- Create index "user_oauth_tokens_user_org_issuer_key" to table: "user_oauth_tokens"
CREATE UNIQUE INDEX "user_oauth_tokens_user_org_issuer_key" ON "user_oauth_tokens" ("user_id", "organization_id", "oauth_server_issuer") WHERE (deleted IS FALSE);
