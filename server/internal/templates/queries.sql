-- name: PeekTemplateByID :one
SELECT id, history_id, name
FROM prompt_templates
WHERE project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
ORDER BY id DESC
LIMIT 1;

-- name: PeekTemplatesByNames :many
SELECT DISTINCT ON (pt.project_id, pt.name) pt.id, pt.history_id, pt.name
FROM prompt_templates pt
WHERE pt.project_id = @project_id
  AND pt.name = ANY(@names::TEXT[])
  AND pt.deleted IS FALSE
ORDER BY pt.project_id, pt.name, pt.id DESC;

-- name: CreateTemplate :one
INSERT INTO prompt_templates (
  project_id,
  history_id,
  tool_urn,
  name,
  prompt,
  description,
  arguments,
  engine,
  kind,
  tools_hint
)
SELECT
  @project_id,
  generate_uuidv7(),
  @tool_urn,
  @name,
  @prompt,
  NULLIF(@description, ''),
  @arguments,
  @engine,
  @kind,
  @tools_hint
RETURNING id;

-- name: UpdateTemplate :one
INSERT INTO prompt_templates (
  project_id,
  history_id,
  predecessor_id,
  tool_urn,
  name,
  prompt,
  description,
  arguments,
  engine,
  kind,
  tools_hint
)
SELECT
  c.project_id,
  c.history_id,
  c.id,
  COALESCE(sqlc.narg(tool_urn), c.tool_urn),
  c.name,
  COALESCE(sqlc.narg(prompt), c.prompt),
  NULLIF(sqlc.narg(description), ''),
  sqlc.narg(arguments),
  COALESCE(NULLIF(sqlc.narg(engine), ''), c.engine),
  COALESCE(NULLIF(sqlc.narg(kind), ''), c.kind),
  COALESCE(sqlc.narg(tools_hint), ARRAY[]::TEXT[])
FROM prompt_templates c
WHERE project_id = @project_id
  AND id = @id
  AND (
    (NULLIF(sqlc.narg(prompt), '') IS NOT NULL AND sqlc.narg(prompt) != c.prompt)
    OR (NULLIF(sqlc.narg(description), '') IS NOT NULL AND sqlc.narg(description) != c.description)
    OR (sqlc.narg(arguments) != c.arguments)
    OR (NULLIF(sqlc.narg(engine), '') IS NOT NULL AND sqlc.narg(engine) != c.engine)
    OR (NULLIF(sqlc.narg(kind), '') IS NOT NULL AND NULLIF(sqlc.narg(kind), '') != c.kind)
    OR (sqlc.narg(tools_hint) IS DISTINCT FROM c.tools_hint)
  )
RETURNING id;

-- name: GetTemplateByID :one
SELECT *
FROM prompt_templates pt
WHERE
  pt.project_id = @project_id
  AND pt.id = @id
  AND pt.deleted IS FALSE
ORDER BY pt.created_at DESC
LIMIT 1;

-- name: GetTemplateByName :one
SELECT *
FROM prompt_templates pt
WHERE
  pt.project_id = @project_id
  AND pt.name = @name
  AND pt.deleted IS FALSE
ORDER BY pt.created_at DESC
LIMIT 1;

-- name: ListTemplates :many
SELECT DISTINCT ON (pt.project_id, pt.name) *
FROM prompt_templates pt
WHERE pt.project_id = @project_id
  AND pt.deleted IS FALSE
ORDER BY pt.project_id, pt.name, pt.created_at DESC, pt.id DESC;

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