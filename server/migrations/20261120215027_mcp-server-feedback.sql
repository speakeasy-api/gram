-- Create "mcp_server_feedback" table
CREATE TABLE "mcp_server_feedback" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "mcp_server_id" uuid NOT NULL,
  "content" text NOT NULL,
  "severity" text NULL,
  "workaround" text NULL,
  "intention" text NULL,
  "tool_name" text NULL,
  "model" text NULL,
  "session_id" text NULL,
  "user_id" text NULL,
  "external_user_id" text NULL,
  "client_name" text NULL,
  "client_version" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_server_feedback_mcp_server_fkey" FOREIGN KEY ("project_id", "mcp_server_id") REFERENCES "mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_server_feedback_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_server_feedback_content_check" CHECK ((content <> ''::text) AND (char_length(content) <= 2000)),
  CONSTRAINT "mcp_server_feedback_intention_check" CHECK ((intention <> ''::text) AND (char_length(intention) <= 1000)),
  CONSTRAINT "mcp_server_feedback_model_check" CHECK ((model <> ''::text) AND (char_length(model) <= 200)),
  CONSTRAINT "mcp_server_feedback_severity_check" CHECK (severity <> ''::text),
  CONSTRAINT "mcp_server_feedback_tool_name_check" CHECK ((tool_name <> ''::text) AND (char_length(tool_name) <= 100)),
  CONSTRAINT "mcp_server_feedback_workaround_check" CHECK ((workaround <> ''::text) AND (char_length(workaround) <= 1000))
);
-- Create index "mcp_server_feedback_project_id_mcp_server_id_created_at_id_idx" to table: "mcp_server_feedback"
CREATE INDEX "mcp_server_feedback_project_id_mcp_server_id_created_at_id_idx" ON "mcp_server_feedback" ("project_id", "mcp_server_id", "created_at" DESC, "id" DESC);
-- Set comment to column: "id" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."project_id" IS '@access: confidential';
-- Set comment to column: "mcp_server_id" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "content" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."content" IS 'The report itself, written by the agent.
@access: opaque-restricted';
-- Set comment to column: "severity" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."severity" IS 'blocked, degraded or cosmetic; NULL when the agent gave none.
@access: confidential';
-- Set comment to column: "workaround" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."workaround" IS 'What the agent did instead, in its own words.
@access: opaque-restricted';
-- Set comment to column: "intention" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."intention" IS 'What the agent was trying to do, in its own words.
@access: opaque-restricted';
-- Set comment to column: "tool_name" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."tool_name" IS 'The tool the agent says the report is about. Not checked against the server''s tools.
@access: opaque-confidential';
-- Set comment to column: "model" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."model" IS 'Model and host product as the agent reported them.
@access: opaque-confidential';
-- Set comment to column: "session_id" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."session_id" IS 'MCP session the call arrived on, when the client sent one.
@access: opaque-confidential';
-- Set comment to column: "user_id" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."user_id" IS 'Speakeasy users.id of the caller, when the caller is a member of the server''s organization.
@access: confidential';
-- Set comment to column: "external_user_id" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."external_user_id" IS 'The caller''s subject in the identity provider the server signs people in with.
@access: confidential-pii';
-- Set comment to column: "client_name" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."client_name" IS 'MCP client name reported at the handshake.
@access: opaque-confidential';
-- Set comment to column: "client_version" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."client_version" IS 'MCP client version reported at the handshake.
@access: opaque-confidential';
-- Set comment to column: "created_at" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_server_feedback"
COMMENT ON COLUMN "mcp_server_feedback"."updated_at" IS '@access: confidential';
-- Create "mcp_server_feedback_configs" table
CREATE TABLE "mcp_server_feedback_configs" (
  "mcp_server_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "enabled" boolean NOT NULL,
  "tool_name" text NULL,
  "tool_description" text NULL,
  "updated_by_user_id" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("mcp_server_id"),
  CONSTRAINT "mcp_server_feedback_configs_mcp_server_fkey" FOREIGN KEY ("project_id", "mcp_server_id") REFERENCES "mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_server_feedback_configs_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_server_feedback_configs_tool_description_check" CHECK ((tool_description <> ''::text) AND (char_length(tool_description) <= 2000)),
  CONSTRAINT "mcp_server_feedback_configs_tool_name_check" CHECK ((tool_name <> ''::text) AND (char_length(tool_name) <= 64))
);
-- Create index "mcp_server_feedback_configs_project_id_mcp_server_id_idx" to table: "mcp_server_feedback_configs"
CREATE INDEX "mcp_server_feedback_configs_project_id_mcp_server_id_idx" ON "mcp_server_feedback_configs" ("project_id", "mcp_server_id");
-- Set comment to column: "mcp_server_id" on table: "mcp_server_feedback_configs"
COMMENT ON COLUMN "mcp_server_feedback_configs"."mcp_server_id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "mcp_server_feedback_configs"
COMMENT ON COLUMN "mcp_server_feedback_configs"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "mcp_server_feedback_configs"
COMMENT ON COLUMN "mcp_server_feedback_configs"."project_id" IS '@access: confidential';
-- Set comment to column: "enabled" on table: "mcp_server_feedback_configs"
COMMENT ON COLUMN "mcp_server_feedback_configs"."enabled" IS '@access: confidential';
-- Set comment to column: "tool_name" on table: "mcp_server_feedback_configs"
COMMENT ON COLUMN "mcp_server_feedback_configs"."tool_name" IS 'Name the feedback tool is listed under, when an admin replaced the default.
@access: opaque-confidential';
-- Set comment to column: "tool_description" on table: "mcp_server_feedback_configs"
COMMENT ON COLUMN "mcp_server_feedback_configs"."tool_description" IS 'Description agents read for the feedback tool, when an admin replaced the default.
@access: opaque-restricted';
-- Set comment to column: "updated_by_user_id" on table: "mcp_server_feedback_configs"
COMMENT ON COLUMN "mcp_server_feedback_configs"."updated_by_user_id" IS 'Speakeasy users.id of the person who last changed these settings; NULL when an API key did.
@access: confidential';
-- Set comment to column: "created_at" on table: "mcp_server_feedback_configs"
COMMENT ON COLUMN "mcp_server_feedback_configs"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "mcp_server_feedback_configs"
COMMENT ON COLUMN "mcp_server_feedback_configs"."updated_at" IS '@access: confidential';
