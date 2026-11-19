-- Create "access_requests" table
CREATE TABLE "access_requests" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "requester_user_id" text NOT NULL,
  "scope" text NOT NULL,
  "selector" jsonb NOT NULL,
  "resource_name" text NULL,
  "message" text NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "decided_by_user_id" text NULL,
  "decided_via" text NULL,
  "decided_at" timestamptz NULL,
  "temporary_grant_id" uuid NULL,
  "challenge_ids" text[] NOT NULL DEFAULT '{}',
  "expires_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "access_requests_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "access_requests_selector_check" CHECK ((jsonb_typeof(selector) = 'object'::text) AND (selector <> '{}'::jsonb))
);
-- Create index "access_requests_open_request_key" to table: "access_requests"
CREATE UNIQUE INDEX "access_requests_open_request_key" ON "access_requests" ("organization_id", "requester_user_id", "scope", "selector") WHERE ((deleted IS FALSE) AND (status = 'pending'::text));
-- Create index "access_requests_org_requester_created_at_idx" to table: "access_requests"
CREATE INDEX "access_requests_org_requester_created_at_idx" ON "access_requests" ("organization_id", "requester_user_id", "created_at" DESC) WHERE (deleted IS FALSE);
-- Create index "access_requests_org_status_created_at_idx" to table: "access_requests"
CREATE INDEX "access_requests_org_status_created_at_idx" ON "access_requests" ("organization_id", "status", "created_at" DESC) WHERE (deleted IS FALSE);
-- Create index "access_requests_organization_id_id_key" to table: "access_requests"
CREATE UNIQUE INDEX "access_requests_organization_id_id_key" ON "access_requests" ("organization_id", "id");
-- Set comment to table: "access_requests"
COMMENT ON TABLE "access_requests" IS 'Requests from members for access they were denied, decided by an organization admin. An approval creates a temporary grant.';
-- Set comment to column: "id" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."organization_id" IS '@access: confidential';
-- Set comment to column: "requester_user_id" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."requester_user_id" IS 'The Speakeasy user who asked for access.
@access: confidential';
-- Set comment to column: "scope" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."scope" IS 'The scope asked for, e.g. "project:read".
@access: confidential';
-- Set comment to column: "selector" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."selector" IS 'Where the scope applies, in the shape of principal_grants.selectors.
@access: opaque-restricted';
-- Set comment to column: "resource_name" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."resource_name" IS 'Name of the resource the request is for, as it was when requested.
@access: opaque-confidential';
-- Set comment to column: "message" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."message" IS 'Free-form note from the requester to the admins.
@access: opaque-restricted';
-- Set comment to column: "status" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."status" IS 'pending, approved, denied, expired or withdrawn.
@access: confidential';
-- Set comment to column: "decided_by_user_id" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."decided_by_user_id" IS 'The admin who approved or denied the request.
@access: confidential';
-- Set comment to column: "decided_via" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."decided_via" IS 'Where the decision came from: dashboard or slack.
@access: confidential';
-- Set comment to column: "decided_at" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."decided_at" IS '@access: confidential';
-- Set comment to column: "temporary_grant_id" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."temporary_grant_id" IS 'The principal_grants row an approval created. Soft reference: the grant may be revoked later.
@access: confidential';
-- Set comment to column: "challenge_ids" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."challenge_ids" IS 'IDs of the denied challenges in the ClickHouse authz_challenges table that this request covers.
@access: opaque-confidential';
-- Set comment to column: "expires_at" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."expires_at" IS 'When a pending request lapses.
@access: confidential';
-- Set comment to column: "created_at" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "access_requests"
COMMENT ON COLUMN "access_requests"."deleted" IS '@access: confidential';
-- Create "access_request_messages" table
CREATE TABLE "access_request_messages" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "access_request_id" uuid NOT NULL,
  "provider" text NOT NULL,
  "kind" text NOT NULL,
  "recipient_user_id" text NOT NULL,
  "slack_team_id" text NULL,
  "slack_user_id" text NULL,
  "slack_channel_id" text NULL,
  "slack_message_ts" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "access_request_messages_organization_id_access_request_id_fkey" FOREIGN KEY ("organization_id", "access_request_id") REFERENCES "access_requests" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "access_request_messages_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "access_request_messages_organization_id_access_request_id_idx" to table: "access_request_messages"
CREATE INDEX "access_request_messages_organization_id_access_request_id_idx" ON "access_request_messages" ("organization_id", "access_request_id");
-- Create index "access_request_messages_request_recipient_key" to table: "access_request_messages"
CREATE UNIQUE INDEX "access_request_messages_request_recipient_key" ON "access_request_messages" ("access_request_id", "provider", "recipient_user_id") WHERE (deleted IS FALSE);
-- Create index "access_request_messages_slack_message_idx" to table: "access_request_messages"
CREATE INDEX "access_request_messages_slack_message_idx" ON "access_request_messages" ("slack_team_id", "slack_channel_id", "slack_message_ts") WHERE (deleted IS FALSE);
-- Set comment to table: "access_request_messages"
COMMENT ON TABLE "access_request_messages" IS 'Notifications sent for an access request, such as one Slack DM per admin.';
-- Set comment to column: "id" on table: "access_request_messages"
COMMENT ON COLUMN "access_request_messages"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "access_request_messages"
COMMENT ON COLUMN "access_request_messages"."organization_id" IS '@access: confidential';
-- Set comment to column: "access_request_id" on table: "access_request_messages"
COMMENT ON COLUMN "access_request_messages"."access_request_id" IS '@access: confidential';
-- Set comment to column: "provider" on table: "access_request_messages"
COMMENT ON COLUMN "access_request_messages"."provider" IS '@access: confidential';
-- Set comment to column: "kind" on table: "access_request_messages"
COMMENT ON COLUMN "access_request_messages"."kind" IS '@access: confidential';
-- Set comment to column: "recipient_user_id" on table: "access_request_messages"
COMMENT ON COLUMN "access_request_messages"."recipient_user_id" IS '@access: confidential';
-- Set comment to column: "slack_team_id" on table: "access_request_messages"
COMMENT ON COLUMN "access_request_messages"."slack_team_id" IS '@access: confidential';
-- Set comment to column: "slack_user_id" on table: "access_request_messages"
COMMENT ON COLUMN "access_request_messages"."slack_user_id" IS '@access: confidential-pii';
-- Set comment to column: "slack_channel_id" on table: "access_request_messages"
COMMENT ON COLUMN "access_request_messages"."slack_channel_id" IS '@access: confidential';
-- Set comment to column: "slack_message_ts" on table: "access_request_messages"
COMMENT ON COLUMN "access_request_messages"."slack_message_ts" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "access_request_messages"
COMMENT ON COLUMN "access_request_messages"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "access_request_messages"
COMMENT ON COLUMN "access_request_messages"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "access_request_messages"
COMMENT ON COLUMN "access_request_messages"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "access_request_messages"
COMMENT ON COLUMN "access_request_messages"."deleted" IS '@access: confidential';
