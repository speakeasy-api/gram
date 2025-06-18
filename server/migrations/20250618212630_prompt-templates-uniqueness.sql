-- atlas:txmode none

-- Drop index "prompt_templates_project_id_name_key" from table: "prompt_templates"
DROP INDEX CONCURRENTLY "prompt_templates_project_id_name_key";
-- Create index "prompt_templates_project_id_name_key" to table: "prompt_templates"
CREATE UNIQUE INDEX CONCURRENTLY "prompt_templates_project_id_name_key" ON "prompt_templates" ("project_id", "name", (COALESCE(predecessor_id, '00000000-0000-0000-0000-000000000000'::uuid))) WHERE (deleted IS FALSE);
