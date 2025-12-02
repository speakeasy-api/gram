-- Drop index "tool_variations_scoped_src_tool_urn_key" from table: "tool_variations"
DROP INDEX CONCURRENTLY "tool_variations_scoped_src_tool_urn_key";
-- Modify "tool_variations" table
ALTER TABLE "tool_variations" ADD COLUMN "predecessor_id" uuid NULL, ADD COLUMN "version" bigint NOT NULL DEFAULT 1;
-- Create index "tool_variations_scoped_src_tool_urn_version_key" to table: "tool_variations"
CREATE UNIQUE INDEX CONCURRENTLY "tool_variations_scoped_src_tool_urn_version_key" ON "tool_variations" ("group_id", "src_tool_urn", "predecessor_id") NULLS NOT DISTINCT WHERE (deleted IS FALSE);
-- Drop index "toolsets_project_id_slug_key" from table: "toolsets"
DROP INDEX CONCURRENTLY "toolsets_project_id_slug_key";
-- Modify "toolsets" table
ALTER TABLE "toolsets" ADD CONSTRAINT "toolsets_editing_mode_check" CHECK (editing_mode = ANY (ARRAY['iteration'::text, 'staging'::text])), ADD COLUMN "parent_toolset_id" uuid NULL, ADD COLUMN "editing_mode" text NOT NULL DEFAULT 'iteration', ADD COLUMN "current_release_id" uuid NULL, ADD COLUMN "predecessor_id" uuid NULL, ADD COLUMN "version" bigint NOT NULL DEFAULT 1, ADD COLUMN "history_id" uuid NOT NULL DEFAULT generate_uuidv7();
-- Create index "toolsets_history_id_version_idx" to table: "toolsets"
CREATE INDEX CONCURRENTLY "toolsets_history_id_version_idx" ON "toolsets" ("history_id", "version" DESC) WHERE (deleted IS FALSE);
-- Create index "toolsets_parent_toolset_id_idx" to table: "toolsets"
CREATE INDEX CONCURRENTLY "toolsets_parent_toolset_id_idx" ON "toolsets" ("parent_toolset_id") WHERE ((parent_toolset_id IS NOT NULL) AND (deleted IS FALSE));
-- Create index "toolsets_project_id_slug_version_key" to table: "toolsets"
CREATE UNIQUE INDEX CONCURRENTLY "toolsets_project_id_slug_version_key" ON "toolsets" ("project_id", "slug", "predecessor_id") NULLS NOT DISTINCT WHERE ((deleted IS FALSE) AND (parent_toolset_id IS NULL));
-- Create "source_states" table
CREATE TABLE "source_states" ("id" uuid NOT NULL DEFAULT generate_uuidv7(), "project_id" uuid NOT NULL, "deployment_id" uuid NOT NULL, "system_source_state_id" uuid NOT NULL, "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY ("id"));
-- Create index "source_states_deployment_system_key" to table: "source_states"
CREATE UNIQUE INDEX "source_states_deployment_system_key" ON "source_states" ("deployment_id", "system_source_state_id");
-- Create "system_source_states" table
CREATE TABLE "system_source_states" ("id" uuid NOT NULL DEFAULT generate_uuidv7(), "project_id" uuid NOT NULL, "prompt_template_ids" uuid[] NOT NULL DEFAULT ARRAY[]::uuid[], "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY ("id"));
-- Create index "system_source_states_project_id_idx" to table: "system_source_states"
CREATE INDEX "system_source_states_project_id_idx" ON "system_source_states" ("project_id", "created_at" DESC);
-- Create "tool_variations_group_versions" table
CREATE TABLE "tool_variations_group_versions" ("id" uuid NOT NULL DEFAULT generate_uuidv7(), "group_id" uuid NOT NULL, "version" bigint NOT NULL, "variation_ids" uuid[] NOT NULL DEFAULT ARRAY[]::uuid[], "predecessor_id" uuid NULL, "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY ("id"), CONSTRAINT "tool_variations_group_versions_group_version_key" UNIQUE ("group_id", "version"), CONSTRAINT "tool_variations_group_versions_predecessor_id_fkey" FOREIGN KEY ("predecessor_id") REFERENCES "tool_variations_group_versions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL);
-- Create index "tool_variations_group_versions_group_id_version_idx" to table: "tool_variations_group_versions"
CREATE INDEX "tool_variations_group_versions_group_id_version_idx" ON "tool_variations_group_versions" ("group_id", "version" DESC);
-- Create "toolset_releases" table
CREATE TABLE "toolset_releases" ("id" uuid NOT NULL DEFAULT generate_uuidv7(), "toolset_id" uuid NOT NULL, "source_state_id" uuid NULL, "toolset_version_id" uuid NOT NULL, "global_variations_version_id" uuid NULL, "toolset_variations_version_id" uuid NULL, "release_number" bigint NOT NULL, "notes" text NULL, "released_by_user_id" text NOT NULL, "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY ("id"), CONSTRAINT "toolset_releases_toolset_id_release_number_key" UNIQUE ("toolset_id", "release_number"));
-- Create index "toolset_releases_toolset_id_idx" to table: "toolset_releases"
CREATE INDEX "toolset_releases_toolset_id_idx" ON "toolset_releases" ("toolset_id", "created_at" DESC);
-- Modify "tool_variations" table
ALTER TABLE "tool_variations" ADD CONSTRAINT "tool_variations_predecessor_id_fkey" FOREIGN KEY ("predecessor_id") REFERENCES "tool_variations" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Modify "toolsets" table
ALTER TABLE "toolsets" ADD CONSTRAINT "toolsets_current_release_id_fkey" FOREIGN KEY ("current_release_id") REFERENCES "toolset_releases" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "toolsets_parent_toolset_id_fkey" FOREIGN KEY ("parent_toolset_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "toolsets_predecessor_id_fkey" FOREIGN KEY ("predecessor_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Modify "source_states" table
ALTER TABLE "source_states" ADD CONSTRAINT "source_states_deployment_id_fkey" FOREIGN KEY ("deployment_id") REFERENCES "deployments" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT, ADD CONSTRAINT "source_states_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "source_states_system_source_state_id_fkey" FOREIGN KEY ("system_source_state_id") REFERENCES "system_source_states" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
-- Modify "system_source_states" table
ALTER TABLE "system_source_states" ADD CONSTRAINT "system_source_states_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Modify "tool_variations_group_versions" table
ALTER TABLE "tool_variations_group_versions" ADD CONSTRAINT "tool_variations_group_versions_group_id_fkey" FOREIGN KEY ("group_id") REFERENCES "tool_variations_groups" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Modify "toolset_releases" table
ALTER TABLE "toolset_releases" ADD CONSTRAINT "toolset_releases_global_variations_version_id_fkey" FOREIGN KEY ("global_variations_version_id") REFERENCES "tool_variations_group_versions" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT, ADD CONSTRAINT "toolset_releases_source_state_id_fkey" FOREIGN KEY ("source_state_id") REFERENCES "source_states" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT, ADD CONSTRAINT "toolset_releases_toolset_id_fkey" FOREIGN KEY ("toolset_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "toolset_releases_toolset_variations_version_id_fkey" FOREIGN KEY ("toolset_variations_version_id") REFERENCES "tool_variations_group_versions" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT, ADD CONSTRAINT "toolset_releases_toolset_version_id_fkey" FOREIGN KEY ("toolset_version_id") REFERENCES "toolset_versions" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
