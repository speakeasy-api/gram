-- Modify "mcp_approval_requests" table
ALTER TABLE "mcp_approval_requests" ADD COLUMN "evidence_changed_at" timestamptz NULL, ADD COLUMN "notified_change_fingerprint" text NULL;
