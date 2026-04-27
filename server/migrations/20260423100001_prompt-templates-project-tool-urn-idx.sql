-- atlas:txmode none

CREATE INDEX CONCURRENTLY IF NOT EXISTS prompt_templates_project_id_tool_urn_idx
ON prompt_templates (project_id, tool_urn)
WHERE deleted IS FALSE;
