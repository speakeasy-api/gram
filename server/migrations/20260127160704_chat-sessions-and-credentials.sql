-- Create "chat_sessions" table
CREATE TABLE "chat_sessions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "external_user_id" text NULL,
  "embed_origin" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "chat_sessions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "chat_sessions_project_id_idx" to table: "chat_sessions"
CREATE INDEX "chat_sessions_project_id_idx" ON "chat_sessions" ("project_id") WHERE (deleted IS FALSE);
-- Create "chat_session_credentials" table
CREATE TABLE "chat_session_credentials" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "chat_session_id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "toolset_id" uuid NOT NULL,
  "access_token_encrypted" bytea NOT NULL,
  "refresh_token_encrypted" bytea NULL,
  "token_type" text NOT NULL DEFAULT 'Bearer',
  "scope" text NULL,
  "expires_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "chat_session_credentials_project_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_session_credentials_session_fkey" FOREIGN KEY ("chat_session_id") REFERENCES "chat_sessions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_session_credentials_toolset_fkey" FOREIGN KEY ("toolset_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "chat_session_credentials_session_toolset_key" to table: "chat_session_credentials"
CREATE UNIQUE INDEX "chat_session_credentials_session_toolset_key" ON "chat_session_credentials" ("chat_session_id", "toolset_id");
