-- atlas:txmode none

-- Modify "admin_mcp_write_events" table
ALTER TABLE "admin_mcp_write_events" ADD CONSTRAINT "admin_mcp_write_events_reason_code_check" CHECK ((reason_code IS NULL) OR (reason_code ~ '^[a-z][a-z0-9_]{0,63}$'::text));
-- Create index "admin_mcp_write_events_oauth_client_id_idx" to table: "admin_mcp_write_events"
CREATE INDEX CONCURRENTLY "admin_mcp_write_events_oauth_client_id_idx" ON "admin_mcp_write_events" ("oauth_client_id") WHERE (oauth_client_id IS NOT NULL);
-- Modify "admin_mcp_write_proposals" table
ALTER TABLE "admin_mcp_write_proposals" ADD CONSTRAINT "admin_mcp_write_proposals_expected_state_digest_check" CHECK (expected_state_digest <> ''::text);
