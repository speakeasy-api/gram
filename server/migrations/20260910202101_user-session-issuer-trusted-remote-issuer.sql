-- atlas:txmode none

-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "jwks" jsonb NULL, ADD COLUMN "jwks_fetched_at" timestamptz NULL, ADD COLUMN "jwks_cache_expires_at" timestamptz NULL, ADD COLUMN "jwks_etag" text NULL;
-- Create index "remote_session_issuers_jwks_cache_expires_at_idx" to table: "remote_session_issuers"
CREATE INDEX CONCURRENTLY "remote_session_issuers_jwks_cache_expires_at_idx" ON "remote_session_issuers" ("jwks_cache_expires_at") WHERE ((jwks_uri IS NOT NULL) AND (deleted IS FALSE));
-- Modify "user_session_issuers" table
ALTER TABLE "user_session_issuers" ADD COLUMN "trusted_remote_session_issuer_id" uuid NULL, ADD CONSTRAINT "user_session_issuers_trusted_remote_session_issuer_id_fkey" FOREIGN KEY ("trusted_remote_session_issuer_id") REFERENCES "remote_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL NOT VALID;
ALTER TABLE "user_session_issuers" VALIDATE CONSTRAINT "user_session_issuers_trusted_remote_session_issuer_id_fkey";
