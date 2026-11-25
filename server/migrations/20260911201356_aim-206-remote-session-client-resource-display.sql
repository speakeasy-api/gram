-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "resource_identifier" text NULL, ADD COLUMN "resource_name" text NULL, ADD COLUMN "resource_documentation" text NULL, ADD COLUMN "resource_policy_uri" text NULL, ADD COLUMN "resource_tos_uri" text NULL;
