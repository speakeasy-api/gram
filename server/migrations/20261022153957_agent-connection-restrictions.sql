-- Set comment to column: "kind" on table: "auth_permission_provisions"
COMMENT ON COLUMN "auth_permission_provisions"."kind" IS 'Which default the grant belongs to: personal_default, shared_client, api_key or attachment from the backfill, or creation_default for the personal default written when the organization was created. Validated in application code.
@access: confidential';
-- Create "agent_connection_restrictions" table
CREATE TABLE "agent_connection_restrictions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "agent_id" uuid NOT NULL,
  "owner_user_id" text NOT NULL,
  "remote_session_client_id" uuid NOT NULL,
  "taken_over_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "agent_connection_restrictions_agent_tenant_fkey" FOREIGN KEY ("organization_id", "agent_id") REFERENCES "agents" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "agent_connection_restrictions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "agent_connection_restrictions_remote_session_client_id_fkey" FOREIGN KEY ("remote_session_client_id") REFERENCES "remote_session_clients" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "agent_connection_restrictions_owner_user_id_check" CHECK (owner_user_id <> ''::text)
);
-- Create index "agent_connection_restrictions_key" to table: "agent_connection_restrictions"
CREATE UNIQUE INDEX "agent_connection_restrictions_key" ON "agent_connection_restrictions" ("organization_id", "agent_id", "owner_user_id", "remote_session_client_id");
-- Create index "agent_connection_restrictions_owner_idx" to table: "agent_connection_restrictions"
CREATE INDEX "agent_connection_restrictions_owner_idx" ON "agent_connection_restrictions" ("organization_id", "owner_user_id");
-- Create index "agent_connection_restrictions_remote_session_client_id_idx" to table: "agent_connection_restrictions"
CREATE INDEX "agent_connection_restrictions_remote_session_client_id_idx" ON "agent_connection_restrictions" ("remote_session_client_id");
-- Set comment to table: "agent_connection_restrictions"
COMMENT ON TABLE "agent_connection_restrictions" IS 'Agents blocked by a connection owner from that owner''s connection to one upstream OAuth client, kept beside the mcp:blocked_auth grant that enforces it.';
-- Set comment to column: "id" on table: "agent_connection_restrictions"
COMMENT ON COLUMN "agent_connection_restrictions"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "agent_connection_restrictions"
COMMENT ON COLUMN "agent_connection_restrictions"."organization_id" IS '@access: confidential';
-- Set comment to column: "agent_id" on table: "agent_connection_restrictions"
COMMENT ON COLUMN "agent_connection_restrictions"."agent_id" IS 'The blocked agent.
@access: confidential';
-- Set comment to column: "owner_user_id" on table: "agent_connection_restrictions"
COMMENT ON COLUMN "agent_connection_restrictions"."owner_user_id" IS 'The connection owner who wrote the restriction; the grant names this user as connection_owner.
@access: confidential-pii';
-- Set comment to column: "remote_session_client_id" on table: "agent_connection_restrictions"
COMMENT ON COLUMN "agent_connection_restrictions"."remote_session_client_id" IS 'The upstream OAuth client the connection uses; the restriction covers every auth client sharing it.
@access: confidential';
-- Set comment to column: "taken_over_at" on table: "agent_connection_restrictions"
COMMENT ON COLUMN "agent_connection_restrictions"."taken_over_at" IS 'When an agent manager wrote the same grant, making it theirs: removing the restriction then keeps the grant. NULL while only the owner holds it.
@access: confidential';
-- Set comment to column: "created_at" on table: "agent_connection_restrictions"
COMMENT ON COLUMN "agent_connection_restrictions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "agent_connection_restrictions"
COMMENT ON COLUMN "agent_connection_restrictions"."updated_at" IS '@access: confidential';
