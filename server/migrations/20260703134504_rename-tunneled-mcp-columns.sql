-- atlas:nolint PG306 CD101
-- PG306/CD101 are false positives here: atlas diffs the rename as drop+add of
-- the FK, but the DDL below is ALTER ... RENAME CONSTRAINT (catalog-only, no
-- scan, no write lock). BC101/BC102 (backward-incompatible rename) are left
-- visible on purpose — the rename is coordinated with the code deploy.
-- Rename the tunnelled_* MCP objects to the single-l "tunneled" spelling used
-- across the Go code, SDK, and dashboard. In-place ALTER ... RENAME (no drops):
-- the values are all NULL / the table is empty (feature unreleased), and renames
-- are non-destructive. Atlas cannot infer renames via migrate diff, so this
-- migration is authored explicitly and atlas.sum re-hashed with the Atlas CLI.

-- billing_metadata.tunnelled_mcp_server_limit
ALTER TABLE "billing_metadata" RENAME COLUMN "tunnelled_mcp_server_limit" TO "tunneled_mcp_server_limit";
ALTER TABLE "billing_metadata" RENAME CONSTRAINT "billing_metadata_tunnelled_mcp_server_limit_check" TO "billing_metadata_tunneled_mcp_server_limit_check";
COMMENT ON COLUMN "billing_metadata"."tunneled_mcp_server_limit" IS 'Contracted org-level cap for tunneled MCP server sources. NULL means use the finite plan default.';

-- tunnelled_mcp_servers table, its constraints and indexes
ALTER TABLE "tunnelled_mcp_servers" RENAME TO "tunneled_mcp_servers";
ALTER TABLE "tunneled_mcp_servers" RENAME CONSTRAINT "tunnelled_mcp_servers_pkey" TO "tunneled_mcp_servers_pkey";
ALTER TABLE "tunneled_mcp_servers" RENAME CONSTRAINT "tunnelled_mcp_servers_project_id_fkey" TO "tunneled_mcp_servers_project_id_fkey";
ALTER TABLE "tunneled_mcp_servers" RENAME CONSTRAINT "tunnelled_mcp_servers_name_check" TO "tunneled_mcp_servers_name_check";
ALTER TABLE "tunneled_mcp_servers" RENAME CONSTRAINT "tunnelled_mcp_servers_key_hash_check" TO "tunneled_mcp_servers_key_hash_check";
ALTER TABLE "tunneled_mcp_servers" RENAME CONSTRAINT "tunnelled_mcp_servers_key_prefix_check" TO "tunneled_mcp_servers_key_prefix_check";
ALTER TABLE "tunneled_mcp_servers" RENAME CONSTRAINT "tunnelled_mcp_servers_status_check" TO "tunneled_mcp_servers_status_check";
ALTER TABLE "tunneled_mcp_servers" RENAME CONSTRAINT "tunnelled_mcp_servers_agent_version_check" TO "tunneled_mcp_servers_agent_version_check";
ALTER INDEX "tunnelled_mcp_servers_project_id_idx" RENAME TO "tunneled_mcp_servers_project_id_idx";
ALTER INDEX "tunnelled_mcp_servers_project_id_name_key" RENAME TO "tunneled_mcp_servers_project_id_name_key";
ALTER INDEX "tunnelled_mcp_servers_key_hash_key" RENAME TO "tunneled_mcp_servers_key_hash_key";
COMMENT ON TABLE "tunneled_mcp_servers" IS 'Customer-hosted MCP server sources that connect to Gram through outbound tunnels.';
COMMENT ON COLUMN "tunneled_mcp_servers"."id" IS 'Stable UUID for the tunneled MCP source. Used by management APIs, dashboard routes, and Redis connection cache keys.';
COMMENT ON COLUMN "tunneled_mcp_servers"."project_id" IS 'Project that owns this tunneled MCP source. All management queries are scoped by project_id.';
COMMENT ON COLUMN "tunneled_mcp_servers"."name" IS 'User-facing display name for the tunneled MCP source.';
COMMENT ON COLUMN "tunneled_mcp_servers"."key_hash" IS 'Hash of the one-time tunnel key. Used for future tunnel authentication without storing the plaintext key.';
COMMENT ON COLUMN "tunneled_mcp_servers"."key_prefix" IS 'Non-secret prefix of the tunnel key shown in the UI so users can identify which key/source they are using.';
COMMENT ON COLUMN "tunneled_mcp_servers"."status" IS 'Durable lifecycle state for the source: created, active, or revoked. Live connection state is derived from Redis.';
COMMENT ON COLUMN "tunneled_mcp_servers"."agent_version" IS 'Last persisted tunnel agent version reported for this source. Per-connection agent versions are stored in Redis.';
COMMENT ON COLUMN "tunneled_mcp_servers"."last_seen_at" IS 'Most recent persisted heartbeat time for the source, used when Redis liveness data is absent or expired.';
COMMENT ON COLUMN "tunneled_mcp_servers"."created_at" IS 'Time when the tunneled MCP source was created.';
COMMENT ON COLUMN "tunneled_mcp_servers"."updated_at" IS 'Time when the durable tunneled MCP source record was last updated.';
COMMENT ON COLUMN "tunneled_mcp_servers"."deleted_at" IS 'Soft-delete timestamp for the tunneled MCP source. NULL means the source is active.';
COMMENT ON COLUMN "tunneled_mcp_servers"."deleted" IS 'Generated soft-delete flag derived from deleted_at and used by partial indexes.';

-- mcp_servers.tunnelled_mcp_server_id (RENAME COLUMN auto-updates the
-- backend_exclusivity_check expression that references it).
ALTER TABLE "mcp_servers" RENAME COLUMN "tunnelled_mcp_server_id" TO "tunneled_mcp_server_id";
ALTER TABLE "mcp_servers" RENAME CONSTRAINT "mcp_servers_tunnelled_mcp_server_id_fkey" TO "mcp_servers_tunneled_mcp_server_id_fkey";
ALTER INDEX "mcp_servers_tunnelled_mcp_server_id_idx" RENAME TO "mcp_servers_tunneled_mcp_server_id_idx";
COMMENT ON COLUMN "mcp_servers"."tunneled_mcp_server_id" IS 'Optional backend reference to a tunneled MCP source. Exactly one of remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id must be set.';
