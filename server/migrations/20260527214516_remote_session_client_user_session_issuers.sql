-- atlas:txmode none

-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ALTER COLUMN "project_id" DROP NOT NULL;
-- Create index "remote_sessions_subject_client_issuer_key" to table: "remote_sessions"
CREATE UNIQUE INDEX CONCURRENTLY "remote_sessions_subject_client_issuer_key" ON "remote_sessions" ("subject_urn", "remote_session_client_id", "user_session_issuer_id") WHERE (deleted IS FALSE);
-- Create "remote_session_client_user_session_issuers" table
CREATE TABLE "remote_session_client_user_session_issuers" (
  "remote_session_client_id" uuid NOT NULL,
  "user_session_issuer_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("remote_session_client_id", "user_session_issuer_id"),
  CONSTRAINT "remote_session_client_user_session_issuers_client_fkey" FOREIGN KEY ("remote_session_client_id") REFERENCES "remote_session_clients" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "remote_session_client_user_session_issuers_issuer_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "remote_session_client_user_session_issuers_issuer_idx" to table: "remote_session_client_user_session_issuers"
CREATE INDEX "remote_session_client_user_session_issuers_issuer_idx" ON "remote_session_client_user_session_issuers" ("user_session_issuer_id", "remote_session_client_id");
-- Create index "remote_session_client_user_session_issuers_one_per_issuer" to table: "remote_session_client_user_session_issuers"
CREATE UNIQUE INDEX "remote_session_client_user_session_issuers_one_per_issuer" ON "remote_session_client_user_session_issuers" ("user_session_issuer_id");
