-- atlas:txmode none

-- Modify "user_session_issuers" table
ALTER TABLE "user_session_issuers" ADD CONSTRAINT "user_session_issuers_classification_check" CHECK (classification = ANY (ARRAY['custom'::text, 'project_default_idp'::text])), ADD COLUMN "classification" text NOT NULL DEFAULT 'custom';
-- Create index "user_session_issuers_project_default_key" to table: "user_session_issuers"
CREATE UNIQUE INDEX CONCURRENTLY "user_session_issuers_project_default_key" ON "user_session_issuers" ("project_id") WHERE ((classification = 'project_default_idp'::text) AND (deleted IS FALSE));
