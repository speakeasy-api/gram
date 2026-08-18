-- atlas:txmode none

-- Modify "mcp_approval_requests" table
ALTER TABLE "mcp_approval_requests" ADD COLUMN "evidence_changed_at" timestamptz NULL, ADD COLUMN "notified_change_fingerprint" text NULL;
-- Create index "mcp_approval_requests_approved_id_idx" to table: "mcp_approval_requests"
CREATE INDEX CONCURRENTLY "mcp_approval_requests_approved_id_idx" ON "mcp_approval_requests" ("id") WHERE ((deleted IS FALSE) AND (status = 'approved'::text));
