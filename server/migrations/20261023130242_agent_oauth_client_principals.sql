-- Modify "user_session_clients" table
ALTER TABLE "user_session_clients" ADD COLUMN "subject_urn" text NULL, ADD COLUMN "authorizer_user_id" text NULL, ADD COLUMN "delegated_grants" jsonb NULL, ADD COLUMN "delegated_grants_version" integer NULL;
-- Set comment to column: "subject_urn" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."subject_urn" IS 'Canonical agent principal bound by authenticated registration.
@access: confidential';
-- Set comment to column: "authorizer_user_id" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."authorizer_user_id" IS 'User authorizing the immutable agent credential profile.
@access: confidential-pii';
-- Set comment to column: "delegated_grants" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."delegated_grants" IS 'Immutable delegated policy for an agent client.
@access: opaque-restricted';
-- Set comment to column: "delegated_grants_version" on table: "user_session_clients"
COMMENT ON COLUMN "user_session_clients"."delegated_grants_version" IS 'Delegated policy encoding version.
@access: confidential';
