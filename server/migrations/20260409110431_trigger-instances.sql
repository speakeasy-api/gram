-- Create "trigger_instances" table
CREATE TABLE "trigger_instances" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "definition_slug" text NOT NULL,
  "name" text NOT NULL,
  "environment_id" uuid NULL,
  "target_kind" text NOT NULL,
  "target_ref" text NOT NULL,
  "target_display" text NOT NULL,
  "config_json" jsonb NOT NULL DEFAULT '{}',
  "status" text NOT NULL DEFAULT 'active',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "trigger_instances_environment_id_fkey" FOREIGN KEY ("environment_id") REFERENCES "environments" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "trigger_instances_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "trigger_instances_definition_slug_check" CHECK ((definition_slug <> ''::text) AND (char_length(definition_slug) <= 60)),
  CONSTRAINT "trigger_instances_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 120)),
  CONSTRAINT "trigger_instances_status_check" CHECK (status = ANY (ARRAY['active'::text, 'paused'::text])),
  CONSTRAINT "trigger_instances_target_display_check" CHECK ((target_display <> ''::text) AND (char_length(target_display) <= 255)),
  CONSTRAINT "trigger_instances_target_kind_check" CHECK ((target_kind <> ''::text) AND (char_length(target_kind) <= 60)),
  CONSTRAINT "trigger_instances_target_ref_check" CHECK ((target_ref <> ''::text) AND (char_length(target_ref) <= 255))
);
-- Create index "trigger_instances_environment_id_idx" to table: "trigger_instances"
CREATE INDEX "trigger_instances_environment_id_idx" ON "trigger_instances" ("environment_id") WHERE (deleted IS FALSE);
-- Create index "trigger_instances_project_id_idx" to table: "trigger_instances"
CREATE INDEX "trigger_instances_project_id_idx" ON "trigger_instances" ("project_id", "created_at" DESC) WHERE (deleted IS FALSE);
