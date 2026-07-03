-- atlas:txmode none

-- Modify "billing_metadata" table
ALTER TABLE "billing_metadata" ADD CONSTRAINT "billing_metadata_tunneled_mcp_server_limit_check" CHECK ((tunneled_mcp_server_limit IS NULL) OR (tunneled_mcp_server_limit >= 0)), ADD COLUMN "tunneled_mcp_server_limit" integer NULL;
-- Create "tunneled_mcp_servers" table
CREATE TABLE "tunneled_mcp_servers" (
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
  CONSTRAINT "tunneled_mcp_servers_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "tunneled_mcp_servers_agent_version_check" CHECK ((agent_version IS NULL) OR (agent_version <> ''::text)),
  CONSTRAINT "tunneled_mcp_servers_key_hash_check" CHECK (key_hash <> ''::text),
  CONSTRAINT "tunneled_mcp_servers_key_prefix_check" CHECK (key_prefix <> ''::text),
  CONSTRAINT "tunneled_mcp_servers_name_check" CHECK (name <> ''::text),
  CONSTRAINT "tunneled_mcp_servers_status_check" CHECK (status = ANY (ARRAY['created'::text, 'active'::text, 'revoked'::text]))
);
-- Create index "tunneled_mcp_servers_key_hash_key" to table: "tunneled_mcp_servers"
CREATE UNIQUE INDEX "tunneled_mcp_servers_key_hash_key" ON "tunneled_mcp_servers" ("key_hash") WHERE (deleted IS FALSE);
-- Create index "tunneled_mcp_servers_project_id_idx" to table: "tunneled_mcp_servers"
CREATE INDEX "tunneled_mcp_servers_project_id_idx" ON "tunneled_mcp_servers" ("project_id") WHERE (deleted IS FALSE);
-- Create index "tunneled_mcp_servers_project_id_name_key" to table: "tunneled_mcp_servers"
CREATE UNIQUE INDEX "tunneled_mcp_servers_project_id_name_key" ON "tunneled_mcp_servers" ("project_id", "name") WHERE (deleted IS FALSE);
-- Set comment to table: "tunneled_mcp_servers"
COMMENT ON TABLE "tunneled_mcp_servers" IS 'Customer-hosted MCP server sources that connect to Gram through outbound tunnels.';
-- Set comment to column: "id" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."id" IS 'Stable UUID for the tunneled MCP source. Used by management APIs, dashboard routes, and Redis connection cache keys.';
-- Set comment to column: "project_id" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."project_id" IS 'Project that owns this tunneled MCP source. All management queries are scoped by project_id.';
-- Set comment to column: "name" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."name" IS 'User-facing display name for the tunneled MCP source.';
-- Set comment to column: "key_hash" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."key_hash" IS 'Hash of the one-time tunnel key. Used for future tunnel authentication without storing the plaintext key.';
-- Set comment to column: "key_prefix" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."key_prefix" IS 'Non-secret prefix of the tunnel key shown in the UI so users can identify which key/source they are using.';
-- Set comment to column: "status" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."status" IS 'Durable lifecycle state for the source: created, active, or revoked. Live connection state is derived from Redis.';
-- Set comment to column: "agent_version" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."agent_version" IS 'Last persisted tunnel agent version reported for this source. Per-connection agent versions are stored in Redis.';
-- Set comment to column: "last_seen_at" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."last_seen_at" IS 'Most recent persisted heartbeat time for the source, used when Redis liveness data is absent or expired.';
-- Set comment to column: "created_at" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."created_at" IS 'Time when the tunneled MCP source was created.';
-- Set comment to column: "updated_at" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."updated_at" IS 'Time when the durable tunneled MCP source record was last updated.';
-- Set comment to column: "deleted_at" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."deleted_at" IS 'Soft-delete timestamp for the tunneled MCP source. NULL means the source is active.';
-- Set comment to column: "deleted" on table: "tunneled_mcp_servers"
COMMENT ON COLUMN "tunneled_mcp_servers"."deleted" IS 'Generated soft-delete flag derived from deleted_at and used by partial indexes.';
-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" DROP CONSTRAINT "mcp_servers_backend_exclusivity_check", ADD CONSTRAINT "mcp_servers_backend_exclusivity_check" CHECK (num_nonnulls(remote_mcp_server_id, tunnelled_mcp_server_id, tunneled_mcp_server_id, toolset_id) = 1), ADD COLUMN "tunneled_mcp_server_id" uuid NULL, ADD CONSTRAINT "mcp_servers_tunneled_mcp_server_id_fkey" FOREIGN KEY ("tunneled_mcp_server_id") REFERENCES "tunneled_mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
-- Create index "mcp_servers_tunneled_mcp_server_id_idx" to table: "mcp_servers"
CREATE INDEX CONCURRENTLY "mcp_servers_tunneled_mcp_server_id_idx" ON "mcp_servers" ("tunneled_mcp_server_id") WHERE (tunneled_mcp_server_id IS NOT NULL);
-- Set comment to column: "tunnelled_mcp_server_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."tunnelled_mcp_server_id" IS 'Deprecated. Superseded by tunneled_mcp_server_id (single-l); dropped in a later contract migration.';
-- Set comment to column: "tunneled_mcp_server_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."tunneled_mcp_server_id" IS 'Optional backend reference to a tunneled MCP source. Exactly one of remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id must be set.';
