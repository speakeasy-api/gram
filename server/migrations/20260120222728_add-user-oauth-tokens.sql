-- Create "user_oauth_tokens" table
CREATE TABLE "user_oauth_tokens" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "user_id" text NOT NULL,
  "organization_id" text NOT NULL,
  "oauth_server_issuer" text NOT NULL,
  "access_token_encrypted" text NOT NULL,
  "refresh_token_encrypted" text NULL,
  "token_type" text NOT NULL DEFAULT 'Bearer',
  "expires_at" timestamptz NULL,
  "scope" text NULL,
  "provider_name" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "user_oauth_tokens_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_oauth_tokens_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_oauth_tokens_oauth_server_issuer_check" CHECK ((oauth_server_issuer <> ''::text) AND (char_length(oauth_server_issuer) <= 500))
);
-- Create index "user_oauth_tokens_user_org_idx" to table: "user_oauth_tokens"
CREATE INDEX "user_oauth_tokens_user_org_idx" ON "user_oauth_tokens" ("user_id", "organization_id") WHERE (deleted IS FALSE);
-- Create index "user_oauth_tokens_user_org_issuer_key" to table: "user_oauth_tokens"
CREATE UNIQUE INDEX "user_oauth_tokens_user_org_issuer_key" ON "user_oauth_tokens" ("user_id", "organization_id", "oauth_server_issuer") WHERE (deleted IS FALSE);
