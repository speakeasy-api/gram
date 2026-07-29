-- atlas:txmode none

-- Create index "skill_edit_suggestions_project_id_created_at_id_open_idx" to table: "skill_edit_suggestions"
CREATE INDEX CONCURRENTLY "skill_edit_suggestions_project_id_created_at_id_open_idx" ON "skill_edit_suggestions" ("project_id", "created_at" DESC, "id" DESC) WHERE (status = 'open'::text);
