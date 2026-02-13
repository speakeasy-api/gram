-- Create "agent_definitions" table
CREATE TABLE "agent_definitions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "name" text NOT NULL,
  "tool_urn" text NOT NULL,
  "model" text NOT NULL,
  "title" text NULL,
  "description" text NOT NULL,
  "instruction" text NOT NULL,
  "tools" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "agent_definitions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "agent_definitions_description_check" CHECK ((description <> ''::text) AND (char_length(description) <= 1000)),
  CONSTRAINT "agent_definitions_instruction_check" CHECK (instruction <> ''::text),
  CONSTRAINT "agent_definitions_model_check" CHECK ((model <> ''::text) AND (char_length(model) <= 100)),
  CONSTRAINT "agent_definitions_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 100)),
  CONSTRAINT "agent_definitions_title_check" CHECK ((title <> ''::text) AND (char_length(title) <= 200)),
  CONSTRAINT "agent_definitions_tool_urn_check" CHECK (tool_urn <> ''::text)
);
-- Create index "agent_definitions_project_id_deleted_idx" to table: "agent_definitions"
CREATE INDEX "agent_definitions_project_id_deleted_idx" ON "agent_definitions" ("project_id", "deleted", "id" DESC) WHERE (deleted IS FALSE);
-- Create index "agent_definitions_project_id_name_key" to table: "agent_definitions"
CREATE UNIQUE INDEX "agent_definitions_project_id_name_key" ON "agent_definitions" ("project_id", "name") WHERE (deleted IS FALSE);
