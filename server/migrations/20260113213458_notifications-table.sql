-- Modify "external_mcp_tool_definitions" table
ALTER TABLE "external_mcp_tool_definitions" ALTER COLUMN "transport_type" DROP DEFAULT;
-- Create "notifications" table
CREATE TABLE "notifications" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "type" text NOT NULL,
  "level" text NOT NULL,
  "title" text NOT NULL,
  "message" text NULL,
  "actor_user_id" text NULL,
  "resource_type" text NULL,
  "resource_id" text NULL,
  "archived_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "notifications_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "notifications_level_check" CHECK (level = ANY (ARRAY['info'::text, 'success'::text, 'warning'::text, 'error'::text])),
  CONSTRAINT "notifications_message_check" CHECK (char_length(message) <= 2000),
  CONSTRAINT "notifications_resource_id_check" CHECK (char_length(resource_id) <= 100),
  CONSTRAINT "notifications_resource_type_check" CHECK (char_length(resource_type) <= 50),
  CONSTRAINT "notifications_title_check" CHECK ((title <> ''::text) AND (char_length(title) <= 200)),
  CONSTRAINT "notifications_type_check" CHECK (type = ANY (ARRAY['system'::text, 'user_action'::text]))
);
-- Create index "notifications_project_id_created_at_idx" to table: "notifications"
CREATE INDEX "notifications_project_id_created_at_idx" ON "notifications" ("project_id", "created_at" DESC) WHERE (deleted IS FALSE);
