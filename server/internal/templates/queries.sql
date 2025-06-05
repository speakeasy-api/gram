-- name: PeekTemplateByID :one
SELECT id, history_id
FROM prompt_templates
WHERE project_id = @project_id
  AND id = @id
  AND deleted = FALSE
ORDER BY id DESC
LIMIT 1;

-- name: CreateTemplate :one
INSERT INTO prompt_templates (
  project_id,
  history_id,
  predecessor_id,
  name,
  prompt,
  description,
  arguments,
  engine,
  kind,
  tools_hint
) VALUES (
  @project_id,
  @history_id,
  @predecessor_id,
  @name,
  @prompt,
  @description,
  @arguments,
  @engine,
  @kind,
  @tools_hint
)
RETURNING id;

-- name: GetTemplateByID :one
SELECT *
FROM prompt_templates pt
WHERE
  pt.project_id = @project_id
  AND pt.id = @id
  AND pt.deleted = FALSE
LIMIT 1;

-- name: GetTemplateByName :one
SELECT *
FROM prompt_templates pt
WHERE
  pt.project_id = @project_id
  AND pt.name = @name
  AND pt.deleted = FALSE
LIMIT 1;

-- name: ListTemplates :many
SELECT DISTINCT ON (pt.project_id, pt.name) *
FROM prompt_templates pt
WHERE pt.project_id = @project_id
  AND pt.deleted = FALSE
ORDER BY pt.id;

-- name: DeleteTemplateByName :exec
UPDATE prompt_templates
SET deleted_at = clock_timestamp()
WHERE project_id = @project_id
  AND name = @name;

-- name: DeleteTemplateByID :exec
UPDATE prompt_templates
SET deleted_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = @id;