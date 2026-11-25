-- Modify "user_session_issuers" table
ALTER TABLE "user_session_issuers" ADD COLUMN "authorization_server_mode" text NOT NULL DEFAULT 'endpoint', ADD COLUMN "pinned_issuer_url" text NULL;
-- Modify "user_sessions" table
ALTER TABLE "user_sessions" ADD COLUMN "resource" text NULL;
