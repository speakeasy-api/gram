-- Modify "access_requests" table
ALTER TABLE "access_requests" ADD COLUMN "requested_duration_seconds" integer NULL, ADD COLUMN "requested_until" timestamptz NULL;
-- Set comment to column: "requested_duration_seconds" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."requested_duration_seconds" IS 'How long the requester asked to have the access, in seconds. A suggestion; the approver picks the window.
@access: confidential';
-- Set comment to column: "requested_until" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."requested_until" IS 'When the requester asked to have the access until. A suggestion; the approver picks the window.
@access: confidential';
