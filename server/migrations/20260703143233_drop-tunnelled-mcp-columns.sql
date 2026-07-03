-- atlas:txmode none
-- atlas:nolint DS102 DS103
-- Contract step of the tunnelled_ -> tunneled_ rename: drop the deprecated
-- tunnelled_ objects now that all code reads/writes the tunneled_ columns.
-- Values were all NULL (feature unreleased), so the drops lose no data.

-- Drop the mcp_servers partial index concurrently first, so dropping the
-- column below does not take an ACCESS EXCLUSIVE lock on the hot table.
DROP INDEX CONCURRENTLY IF EXISTS "mcp_servers_tunnelled_mcp_server_id_idx";
-- Modify "billing_metadata" table
ALTER TABLE "billing_metadata" DROP CONSTRAINT "billing_metadata_tunnelled_mcp_server_limit_check", DROP COLUMN "tunnelled_mcp_server_limit";
-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" DROP CONSTRAINT "mcp_servers_backend_exclusivity_check", ADD CONSTRAINT "mcp_servers_backend_exclusivity_check" CHECK (num_nonnulls(remote_mcp_server_id, tunneled_mcp_server_id, toolset_id) = 1), DROP COLUMN "tunnelled_mcp_server_id";
-- Drop "tunnelled_mcp_servers" table
DROP TABLE "tunnelled_mcp_servers";
