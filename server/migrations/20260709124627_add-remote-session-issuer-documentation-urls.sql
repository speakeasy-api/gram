-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "service_documentation" text NULL, ADD COLUMN "op_policy_uri" text NULL, ADD COLUMN "op_tos_uri" text NULL, ADD COLUMN "client_setup_documentation_url" text NULL;
