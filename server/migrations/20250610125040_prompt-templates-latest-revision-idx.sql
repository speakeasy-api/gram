-- atlas:txmode none

-- Drop index "prompt_templates_project_id_idx" from table: "prompt_templates"
DROP INDEX CONCURRENTLY "prompt_templates_project_id_idx";
-- Create index "prompt_templates_latest_revision" to table: "prompt_templates"
CREATE INDEX CONCURRENTLY "prompt_templates_latest_revision" ON "prompt_templates" ("project_id", "history_id", "id" DESC) WHERE (deleted IS FALSE);
