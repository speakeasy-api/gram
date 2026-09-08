-- atlas:txmode none

-- Modify "user_session_clients" table
ALTER TABLE "user_session_clients" ALTER COLUMN "project_id" DROP NOT NULL, ADD COLUMN "organization_id" text NULL, ADD CONSTRAINT "user_session_clients_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
ALTER TABLE "user_session_clients" VALIDATE CONSTRAINT "user_session_clients_organization_id_fkey";
-- Create index "user_session_clients_organization_id_idx" to table: "user_session_clients"
CREATE INDEX CONCURRENTLY "user_session_clients_organization_id_idx" ON "user_session_clients" ("organization_id");
-- Modify "user_session_consents" table
ALTER TABLE "user_session_consents" ALTER COLUMN "project_id" DROP NOT NULL, ADD COLUMN "organization_id" text NULL, ADD CONSTRAINT "user_session_consents_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
ALTER TABLE "user_session_consents" VALIDATE CONSTRAINT "user_session_consents_organization_id_fkey";
-- Create index "user_session_consents_organization_id_idx" to table: "user_session_consents"
CREATE INDEX CONCURRENTLY "user_session_consents_organization_id_idx" ON "user_session_consents" ("organization_id");
-- Modify "user_session_issuer_cimd_clients" table
ALTER TABLE "user_session_issuer_cimd_clients" ALTER COLUMN "project_id" DROP NOT NULL, ADD COLUMN "organization_id" text NULL, ADD CONSTRAINT "user_session_issuer_cimd_clients_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
ALTER TABLE "user_session_issuer_cimd_clients" VALIDATE CONSTRAINT "user_session_issuer_cimd_clients_organization_id_fkey";
-- Create index "user_session_issuer_cimd_clients_organization_id_idx" to table: "user_session_issuer_cimd_clients"
CREATE INDEX CONCURRENTLY "user_session_issuer_cimd_clients_organization_id_idx" ON "user_session_issuer_cimd_clients" ("organization_id");
-- Modify "user_session_issuers" table
ALTER TABLE "user_session_issuers" ALTER COLUMN "project_id" DROP NOT NULL, ADD COLUMN "organization_id" text NULL, ADD CONSTRAINT "user_session_issuers_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
ALTER TABLE "user_session_issuers" VALIDATE CONSTRAINT "user_session_issuers_organization_id_fkey";
-- Create index "user_session_issuers_organization_id_idx" to table: "user_session_issuers"
CREATE INDEX CONCURRENTLY "user_session_issuers_organization_id_idx" ON "user_session_issuers" ("organization_id");
-- Modify "user_sessions" table
ALTER TABLE "user_sessions" ALTER COLUMN "project_id" DROP NOT NULL, ADD COLUMN "organization_id" text NULL, ADD CONSTRAINT "user_sessions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
ALTER TABLE "user_sessions" VALIDATE CONSTRAINT "user_sessions_organization_id_fkey";
-- Create index "user_sessions_organization_id_idx" to table: "user_sessions"
CREATE INDEX CONCURRENTLY "user_sessions_organization_id_idx" ON "user_sessions" ("organization_id");
