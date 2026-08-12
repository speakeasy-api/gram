-- atlas:txmode none

-- Create index "mcp_approval_requests_approved_id_idx" to table: "mcp_approval_requests"
CREATE INDEX CONCURRENTLY "mcp_approval_requests_approved_id_idx" ON "mcp_approval_requests" ("id") WHERE ((deleted IS FALSE) AND (status = 'approved'::text));
