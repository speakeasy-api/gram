-- atlas:txmode none

-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ALTER COLUMN "project_id" DROP NOT NULL, ADD COLUMN "organization_id" text NULL, ADD CONSTRAINT "remote_session_issuers_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "remote_session_issuers_organization_id_idx" to table: "remote_session_issuers"
CREATE INDEX CONCURRENTLY "remote_session_issuers_organization_id_idx" ON "remote_session_issuers" ("organization_id") WHERE (deleted IS FALSE);
