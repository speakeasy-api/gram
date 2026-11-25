-- Modify "api_keys" table
ALTER TABLE "api_keys" ADD COLUMN "subject_urn" text NULL, ADD COLUMN "delegated_grants" jsonb NULL, ADD COLUMN "delegated_grants_version" integer NULL, ADD COLUMN "expires_at" timestamptz NULL;
-- Modify "user_sessions" table
ALTER TABLE "user_sessions" ADD COLUMN "authorizer_user_id" text NULL, ADD COLUMN "delegated_grants" jsonb NULL, ADD COLUMN "delegated_grants_version" integer NULL;
