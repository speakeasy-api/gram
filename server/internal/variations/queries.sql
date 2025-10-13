-- name: PokeGlobalToolVariationsGroup :one
SELECT tool_variations_groups.id
FROM tool_variations_groups
INNER JOIN project_tool_variations ON tool_variations_groups.id = project_tool_variations.group_id
WHERE project_tool_variations.project_id = @project_id;

-- name: InitGlobalToolVariationsGroup :one
WITH created AS (
  INSERT INTO tool_variations_groups (
    project_id,
    name,
    description
  )
  SELECT @project_id, @name, @description
  RETURNING id
),
attached AS (
  INSERT INTO project_tool_variations (
    project_id,
    group_id
  )
  SELECT @project_id, (SELECT id FROM created)
)
SELECT id FROM created;

-- name: UpsertToolVariation :one
INSERT INTO tool_variations (
  group_id,
  src_tool_urn,
  src_tool_name,
  confirm,
  confirm_prompt,
  name,
  summary,
  description,
  tags,
  summarizer
) VALUES (
  @group_id,
  @src_tool_urn,
  @src_tool_name,
  @confirm,
  @confirm_prompt,
  @name,
  @summary,
  @description,
  @tags,
  @summarizer
) ON CONFLICT (group_id, src_tool_name) WHERE deleted IS FALSE DO UPDATE SET
  src_tool_urn = EXCLUDED.src_tool_urn,
  confirm = EXCLUDED.confirm,
  confirm_prompt = EXCLUDED.confirm_prompt,
  name = EXCLUDED.name,
  summary = EXCLUDED.summary,
  description = EXCLUDED.description,
  tags = EXCLUDED.tags,
  summarizer = EXCLUDED.summarizer,
  updated_at = clock_timestamp()
RETURNING *;

-- name: ListGlobalToolVariations :many
SELECT sqlc.embed(tool_variations)
FROM tool_variations
INNER JOIN tool_variations_groups
  ON tool_variations.group_id = tool_variations_groups.id
INNER JOIN project_tool_variations
  ON tool_variations_groups.id = project_tool_variations.group_id
WHERE
  project_tool_variations.project_id = @project_id
  AND tool_variations.deleted IS FALSE
ORDER BY tool_variations.id DESC;

-- name: FindGlobalVariationsByToolNames :many
WITH global_group AS (
  SELECT tool_variations_groups.id
  FROM tool_variations_groups
  INNER JOIN project_tool_variations ON tool_variations_groups.id = project_tool_variations.group_id
  WHERE project_tool_variations.project_id = @project_id
  ORDER BY project_tool_variations.id DESC
  LIMIT 1
)
SELECT *
FROM tool_variations
WHERE
  group_id = (SELECT id FROM global_group)
  AND src_tool_name = ANY(@tool_names::text[])
  AND deleted IS FALSE;

-- name: DeleteGlobalToolVariation :one
UPDATE tool_variations SET deleted_at = clock_timestamp()
WHERE tool_variations.id = @id
  AND tool_variations.group_id IN (
    SELECT tool_variations_groups.id
    FROM tool_variations_groups
    INNER JOIN project_tool_variations ON tool_variations_groups.id = project_tool_variations.group_id
    WHERE project_tool_variations.project_id = @project_id
  )
  AND tool_variations.deleted IS FALSE
RETURNING tool_variations.id;
