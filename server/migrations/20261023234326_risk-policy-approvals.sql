-- Modify "risk_policies" table
ALTER TABLE "risk_policies" ADD COLUMN "approval_config" jsonb NULL;
-- Set comment to column: "approval_config" on table: "risk_policies"
COMMENT ON COLUMN "risk_policies"."approval_config" IS '@access: opaque-restricted';
-- Create "risk_policy_approvals" table
CREATE TABLE "risk_policy_approvals" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "risk_policy_id" uuid NOT NULL,
  "policy_version" bigint NOT NULL,
  "token" text NOT NULL,
  "requester_urn" text NOT NULL,
  "requester_user_id" text NULL,
  "surface" text NOT NULL,
  "source_adapter" text NULL,
  "session_id" text NULL,
  "chat_id" uuid NULL,
  "toolset_id" uuid NULL,
  "mcp_server_id" uuid NULL,
  "server_url" text NULL,
  "tool_name" text NOT NULL,
  "call_fingerprint" text NOT NULL,
  "arguments_preview" jsonb NOT NULL DEFAULT '{}',
  "reason" text NULL,
  "status" text NOT NULL DEFAULT 'blocked',
  "requested_scope" jsonb NULL,
  "granted_scope" jsonb NULL,
  "decided_by_urn" text NULL,
  "decided_via" text NULL,
  "decision_note" text NULL,
  "requested_at" timestamptz NULL,
  "decided_at" timestamptz NULL,
  "expires_at" timestamptz NULL,
  "consumed_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "risk_policy_approvals_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "risk_policy_approvals_mcp_server_id_fkey" FOREIGN KEY ("mcp_server_id") REFERENCES "mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "risk_policy_approvals_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_approvals_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_approvals_risk_policy_id_fkey" FOREIGN KEY ("risk_policy_id") REFERENCES "risk_policies" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_approvals_toolset_id_fkey" FOREIGN KEY ("toolset_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "risk_policy_approvals_arguments_preview_check" CHECK (jsonb_typeof(arguments_preview) = 'object'::text)
);
-- Create index "risk_policy_approvals_chat_id_idx" to table: "risk_policy_approvals"
CREATE INDEX "risk_policy_approvals_chat_id_idx" ON "risk_policy_approvals" ("chat_id");
-- Create index "risk_policy_approvals_lookup_idx" to table: "risk_policy_approvals"
CREATE INDEX "risk_policy_approvals_lookup_idx" ON "risk_policy_approvals" ("project_id", "requester_urn", "call_fingerprint", "status") WHERE (deleted IS FALSE);
-- Create index "risk_policy_approvals_mcp_server_id_idx" to table: "risk_policy_approvals"
CREATE INDEX "risk_policy_approvals_mcp_server_id_idx" ON "risk_policy_approvals" ("mcp_server_id");
-- Create index "risk_policy_approvals_open_call_key" to table: "risk_policy_approvals"
CREATE UNIQUE INDEX "risk_policy_approvals_open_call_key" ON "risk_policy_approvals" ("project_id", "risk_policy_id", "requester_urn", "tool_name", "call_fingerprint") WHERE ((deleted IS FALSE) AND (status = ANY (ARRAY['blocked'::text, 'pending'::text])));
-- Create index "risk_policy_approvals_organization_id_idx" to table: "risk_policy_approvals"
CREATE INDEX "risk_policy_approvals_organization_id_idx" ON "risk_policy_approvals" ("organization_id");
-- Create index "risk_policy_approvals_project_id_id_key" to table: "risk_policy_approvals"
CREATE UNIQUE INDEX "risk_policy_approvals_project_id_id_key" ON "risk_policy_approvals" ("project_id", "id");
-- Create index "risk_policy_approvals_project_id_idx" to table: "risk_policy_approvals"
CREATE INDEX "risk_policy_approvals_project_id_idx" ON "risk_policy_approvals" ("project_id");
-- Create index "risk_policy_approvals_project_status_updated_idx" to table: "risk_policy_approvals"
CREATE INDEX "risk_policy_approvals_project_status_updated_idx" ON "risk_policy_approvals" ("project_id", "status", "updated_at" DESC) WHERE (deleted IS FALSE);
-- Create index "risk_policy_approvals_risk_policy_id_idx" to table: "risk_policy_approvals"
CREATE INDEX "risk_policy_approvals_risk_policy_id_idx" ON "risk_policy_approvals" ("risk_policy_id");
-- Create index "risk_policy_approvals_status_expires_at_idx" to table: "risk_policy_approvals"
CREATE INDEX "risk_policy_approvals_status_expires_at_idx" ON "risk_policy_approvals" ("status", "expires_at") WHERE (deleted IS FALSE);
-- Create index "risk_policy_approvals_token_key" to table: "risk_policy_approvals"
CREATE UNIQUE INDEX "risk_policy_approvals_token_key" ON "risk_policy_approvals" ("token");
-- Create index "risk_policy_approvals_toolset_id_idx" to table: "risk_policy_approvals"
CREATE INDEX "risk_policy_approvals_toolset_id_idx" ON "risk_policy_approvals" ("toolset_id");
-- Set comment to table: "risk_policy_approvals"
COMMENT ON TABLE "risk_policy_approvals" IS 'Speakeasy Approvals: a tool call held by a require_approval risk policy, from block through request, decision and consumption.';
-- Set comment to column: "id" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."project_id" IS '@access: confidential';
-- Set comment to column: "risk_policy_id" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."risk_policy_id" IS '@access: confidential';
-- Set comment to column: "policy_version" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."policy_version" IS '@access: confidential';
-- Set comment to column: "token" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."token" IS '@access: secret-restricted';
-- Set comment to column: "requester_urn" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."requester_urn" IS '@access: confidential';
-- Set comment to column: "requester_user_id" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."requester_user_id" IS '@access: confidential';
-- Set comment to column: "surface" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."surface" IS '@access: confidential';
-- Set comment to column: "source_adapter" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."source_adapter" IS '@access: confidential';
-- Set comment to column: "session_id" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."session_id" IS '@access: confidential';
-- Set comment to column: "chat_id" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."chat_id" IS '@access: confidential';
-- Set comment to column: "toolset_id" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."toolset_id" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "server_url" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."server_url" IS '@access: restricted';
-- Set comment to column: "tool_name" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."tool_name" IS '@access: confidential';
-- Set comment to column: "call_fingerprint" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."call_fingerprint" IS '@access: confidential';
-- Set comment to column: "arguments_preview" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."arguments_preview" IS '@access: opaque-restricted';
-- Set comment to column: "reason" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."reason" IS '@access: opaque-restricted';
-- Set comment to column: "status" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."status" IS '@access: confidential';
-- Set comment to column: "requested_scope" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."requested_scope" IS '@access: opaque-confidential';
-- Set comment to column: "granted_scope" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."granted_scope" IS '@access: opaque-confidential';
-- Set comment to column: "decided_by_urn" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."decided_by_urn" IS '@access: confidential';
-- Set comment to column: "decided_via" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."decided_via" IS '@access: confidential';
-- Set comment to column: "decision_note" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."decision_note" IS '@access: opaque-restricted';
-- Set comment to column: "requested_at" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."requested_at" IS '@access: confidential';
-- Set comment to column: "decided_at" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."decided_at" IS '@access: confidential';
-- Set comment to column: "expires_at" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."expires_at" IS '@access: confidential';
-- Set comment to column: "consumed_at" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."consumed_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "risk_policy_approvals"
COMMENT ON COLUMN "risk_policy_approvals"."deleted" IS '@access: confidential';
-- Create "risk_policy_approval_messages" table
CREATE TABLE "risk_policy_approval_messages" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "approval_id" uuid NOT NULL,
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
  CONSTRAINT "risk_policy_approval_messages_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_approval_messages_project_id_approval_id_fkey" FOREIGN KEY ("project_id", "approval_id") REFERENCES "risk_policy_approvals" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "risk_policy_approval_messages_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "risk_policy_approval_messages_approval_recipient_key" to table: "risk_policy_approval_messages"
CREATE UNIQUE INDEX "risk_policy_approval_messages_approval_recipient_key" ON "risk_policy_approval_messages" ("approval_id", "provider", "recipient_user_id") WHERE (deleted IS FALSE);
-- Create index "risk_policy_approval_messages_organization_id_idx" to table: "risk_policy_approval_messages"
CREATE INDEX "risk_policy_approval_messages_organization_id_idx" ON "risk_policy_approval_messages" ("organization_id");
-- Create index "risk_policy_approval_messages_project_id_approval_id_idx" to table: "risk_policy_approval_messages"
CREATE INDEX "risk_policy_approval_messages_project_id_approval_id_idx" ON "risk_policy_approval_messages" ("project_id", "approval_id");
-- Create index "risk_policy_approval_messages_project_id_idx" to table: "risk_policy_approval_messages"
CREATE INDEX "risk_policy_approval_messages_project_id_idx" ON "risk_policy_approval_messages" ("project_id");
-- Create index "risk_policy_approval_messages_slack_message_idx" to table: "risk_policy_approval_messages"
CREATE INDEX "risk_policy_approval_messages_slack_message_idx" ON "risk_policy_approval_messages" ("slack_channel_id", "slack_message_ts") WHERE (deleted IS FALSE);
-- Set comment to table: "risk_policy_approval_messages"
COMMENT ON TABLE "risk_policy_approval_messages" IS 'Notifications sent for a Speakeasy Approvals request, such as one Slack DM per approver.';
-- Set comment to column: "id" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."project_id" IS '@access: confidential';
-- Set comment to column: "approval_id" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."approval_id" IS '@access: confidential';
-- Set comment to column: "provider" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."provider" IS '@access: confidential';
-- Set comment to column: "kind" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."kind" IS '@access: confidential';
-- Set comment to column: "recipient_user_id" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."recipient_user_id" IS '@access: confidential';
-- Set comment to column: "slack_team_id" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."slack_team_id" IS '@access: confidential';
-- Set comment to column: "slack_user_id" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."slack_user_id" IS '@access: confidential';
-- Set comment to column: "slack_channel_id" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."slack_channel_id" IS '@access: confidential';
-- Set comment to column: "slack_message_ts" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."slack_message_ts" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."updated_at" IS '@access: confidential';
-- Set comment to column: "deleted_at" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."deleted_at" IS '@access: confidential';
-- Set comment to column: "deleted" on table: "risk_policy_approval_messages"
COMMENT ON COLUMN "risk_policy_approval_messages"."deleted" IS '@access: confidential';
