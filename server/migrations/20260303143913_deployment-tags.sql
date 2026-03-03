-- Create "deployment_tags" table
CREATE TABLE "deployment_tags" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "name" text NOT NULL,
  "deployment_id" uuid NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "deployment_tags_deployment_id_fkey" FOREIGN KEY ("deployment_id") REFERENCES "deployments" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "deployment_tags_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "deployment_tags_name_check" CHECK ((name <> ''::text) AND (char_length(name) <= 60))
);
-- Create index "deployment_tags_project_id_name_key" to table: "deployment_tags"
CREATE UNIQUE INDEX "deployment_tags_project_id_name_key" ON "deployment_tags" ("project_id", "name");
-- Create "deployment_tag_history" table
CREATE TABLE "deployment_tag_history" (
  "changed_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "changed_by" text NULL,
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "tag_id" uuid NOT NULL,
  "previous_deployment_id" uuid NULL,
  "new_deployment_id" uuid NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "deployment_tag_history_changed_by_fkey" FOREIGN KEY ("changed_by") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "deployment_tag_history_new_deployment_id_fkey" FOREIGN KEY ("new_deployment_id") REFERENCES "deployments" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "deployment_tag_history_previous_deployment_id_fkey" FOREIGN KEY ("previous_deployment_id") REFERENCES "deployments" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "deployment_tag_history_tag_id_fkey" FOREIGN KEY ("tag_id") REFERENCES "deployment_tags" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
