-- Create "external_oauth_client_registrations" table
CREATE TABLE "external_oauth_client_registrations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
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
  CONSTRAINT "external_oauth_client_registrations_client_id_check" CHECK (client_id <> ''::text),
  CONSTRAINT "external_oauth_client_registrations_oauth_server_issuer_check" CHECK (oauth_server_issuer <> ''::text)
);
-- Create index "external_oauth_client_registrations_org_issuer_key" to table: "external_oauth_client_registrations"
CREATE UNIQUE INDEX "external_oauth_client_registrations_org_issuer_key" ON "external_oauth_client_registrations" ("organization_id", "oauth_server_issuer") WHERE (deleted IS FALSE);
