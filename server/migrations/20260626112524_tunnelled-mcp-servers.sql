-- atlas:txmode none

-- Modify "billing_metadata" table
ALTER TABLE "billing_metadata" ADD COLUMN "tunnelled_mcp_server_limit" integer NULL;
ALTER TABLE "billing_metadata" ADD CONSTRAINT "billing_metadata_tunnelled_mcp_server_limit_check" CHECK ((tunnelled_mcp_server_limit IS NULL) OR (tunnelled_mcp_server_limit >= 0)) NOT VALID;
ALTER TABLE "billing_metadata" VALIDATE CONSTRAINT "billing_metadata_tunnelled_mcp_server_limit_check";
-- Set comment to column: "tunnelled_mcp_server_limit" on table: "billing_metadata"
COMMENT ON COLUMN "billing_metadata"."tunnelled_mcp_server_limit" IS 'Contracted org-level cap for tunnelled MCP server sources. NULL means use the finite plan default.';
-- Create "tunnelled_mcp_servers" table
CREATE TABLE "tunnelled_mcp_servers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "name" text NOT NULL,
  "key_hash" text NOT NULL,
  "key_prefix" text NOT NULL,
  "status" text NOT NULL DEFAULT 'created',
  "agent_version" text NULL,
  "last_seen_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "tunnelled_mcp_servers_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "tunnelled_mcp_servers_agent_version_check" CHECK ((agent_version IS NULL) OR (agent_version <> ''::text)),
  CONSTRAINT "tunnelled_mcp_servers_key_hash_check" CHECK (key_hash <> ''::text),
  CONSTRAINT "tunnelled_mcp_servers_key_prefix_check" CHECK (key_prefix <> ''::text),
  CONSTRAINT "tunnelled_mcp_servers_name_check" CHECK (name <> ''::text),
  CONSTRAINT "tunnelled_mcp_servers_status_check" CHECK (status = ANY (ARRAY['created'::text, 'active'::text, 'revoked'::text]))
);
-- Create index "tunnelled_mcp_servers_key_hash_key" to table: "tunnelled_mcp_servers"
CREATE UNIQUE INDEX "tunnelled_mcp_servers_key_hash_key" ON "tunnelled_mcp_servers" ("key_hash") WHERE (deleted IS FALSE);
-- Create index "tunnelled_mcp_servers_project_id_idx" to table: "tunnelled_mcp_servers"
CREATE INDEX "tunnelled_mcp_servers_project_id_idx" ON "tunnelled_mcp_servers" ("project_id") WHERE (deleted IS FALSE);
-- Create index "tunnelled_mcp_servers_project_id_name_key" to table: "tunnelled_mcp_servers"
CREATE UNIQUE INDEX "tunnelled_mcp_servers_project_id_name_key" ON "tunnelled_mcp_servers" ("project_id", "name") WHERE (deleted IS FALSE);
-- Set comment to table: "tunnelled_mcp_servers"
COMMENT ON TABLE "tunnelled_mcp_servers" IS 'Customer-hosted MCP server sources that connect to Gram through outbound tunnels.';
-- Set comment to column: "id" on table: "tunnelled_mcp_servers"
COMMENT ON COLUMN "tunnelled_mcp_servers"."id" IS 'Stable UUID for the tunnelled MCP source. Used by management APIs, dashboard routes, and Redis connection cache keys.';
-- Set comment to column: "project_id" on table: "tunnelled_mcp_servers"
COMMENT ON COLUMN "tunnelled_mcp_servers"."project_id" IS 'Project that owns this tunnelled MCP source. All management queries are scoped by project_id.';
-- Set comment to column: "name" on table: "tunnelled_mcp_servers"
COMMENT ON COLUMN "tunnelled_mcp_servers"."name" IS 'User-facing display name for the tunnelled MCP source.';
-- Set comment to column: "key_hash" on table: "tunnelled_mcp_servers"
COMMENT ON COLUMN "tunnelled_mcp_servers"."key_hash" IS 'Hash of the one-time tunnel key. Used for future tunnel authentication without storing the plaintext key.';
-- Set comment to column: "key_prefix" on table: "tunnelled_mcp_servers"
COMMENT ON COLUMN "tunnelled_mcp_servers"."key_prefix" IS 'Non-secret prefix of the tunnel key shown in the UI so users can identify which key/source they are using.';
-- Set comment to column: "status" on table: "tunnelled_mcp_servers"
COMMENT ON COLUMN "tunnelled_mcp_servers"."status" IS 'Durable lifecycle state for the source: created, active, or revoked. Live connection state is derived from Redis.';
-- Set comment to column: "agent_version" on table: "tunnelled_mcp_servers"
COMMENT ON COLUMN "tunnelled_mcp_servers"."agent_version" IS 'Last persisted tunnel agent version reported for this source. Per-connection agent versions are stored in Redis.';
-- Set comment to column: "last_seen_at" on table: "tunnelled_mcp_servers"
COMMENT ON COLUMN "tunnelled_mcp_servers"."last_seen_at" IS 'Most recent persisted heartbeat time for the source, used when Redis liveness data is absent or expired.';
-- Set comment to column: "created_at" on table: "tunnelled_mcp_servers"
COMMENT ON COLUMN "tunnelled_mcp_servers"."created_at" IS 'Time when the tunnelled MCP source was created.';
-- Set comment to column: "updated_at" on table: "tunnelled_mcp_servers"
COMMENT ON COLUMN "tunnelled_mcp_servers"."updated_at" IS 'Time when the durable tunnelled MCP source record was last updated.';
-- Set comment to column: "deleted_at" on table: "tunnelled_mcp_servers"
COMMENT ON COLUMN "tunnelled_mcp_servers"."deleted_at" IS 'Soft-delete timestamp for the tunnelled MCP source. NULL means the source is active.';
-- Set comment to column: "deleted" on table: "tunnelled_mcp_servers"
COMMENT ON COLUMN "tunnelled_mcp_servers"."deleted" IS 'Generated soft-delete flag derived from deleted_at and used by partial indexes.';
-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" ADD COLUMN "tunnelled_mcp_server_id" uuid NULL;
ALTER TABLE "mcp_servers" ADD CONSTRAINT "mcp_servers_tunnelled_mcp_server_id_fkey" FOREIGN KEY ("tunnelled_mcp_server_id") REFERENCES "tunnelled_mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT NOT VALID;
ALTER TABLE "mcp_servers" VALIDATE CONSTRAINT "mcp_servers_tunnelled_mcp_server_id_fkey";
ALTER TABLE "mcp_servers" DROP CONSTRAINT "mcp_servers_backend_exclusivity_check", ADD CONSTRAINT "mcp_servers_backend_exclusivity_check" CHECK (num_nonnulls(remote_mcp_server_id, tunnelled_mcp_server_id, toolset_id) = 1) NOT VALID;
ALTER TABLE "mcp_servers" VALIDATE CONSTRAINT "mcp_servers_backend_exclusivity_check";
-- Create index "mcp_servers_tunnelled_mcp_server_id_idx" to table: "mcp_servers"
CREATE INDEX CONCURRENTLY "mcp_servers_tunnelled_mcp_server_id_idx" ON "mcp_servers" ("tunnelled_mcp_server_id") WHERE (tunnelled_mcp_server_id IS NOT NULL);
-- Set comment to column: "tunnelled_mcp_server_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."tunnelled_mcp_server_id" IS 'Optional backend reference to a tunnelled MCP source. Exactly one of remote_mcp_server_id, tunnelled_mcp_server_id, or toolset_id must be set.';
