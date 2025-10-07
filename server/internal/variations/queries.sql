-- name: PokeGlobalToolVariationsGroup :one
SELECT tool_variations_groups.id
FROM tool_variations_groups
INNER JOIN project_tool_variations ON tool_variations_groups.id = project_tool_variations.group_id
WHERE project_tool_variations.project_id = @project_id;

-- name: PokeGlobalToolVariationsGroupVersion :one
SELECT tool_variations_groups.version
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
WITH updated_group AS (
  -- Automatically bump the group version
  UPDATE tool_variations_groups tvg
  SET
    version = version + 1,
    updated_at = now()
  WHERE tvg.id = @group_id
  RETURNING tvg.version
),
inserted AS (
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
    summarizer,
    predecessor_id
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
    @summarizer,
    (SELECT id FROM tool_variations
     WHERE group_id = @group_id
       AND src_tool_name = @src_tool_name
       AND deleted IS FALSE
     ORDER BY created_at DESC
     LIMIT 1)
  )
  RETURNING *
)
SELECT * FROM inserted;

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
  AND tool_variations.id NOT IN (
    SELECT DISTINCT predecessor_id 
    FROM tool_variations 
    WHERE predecessor_id IS NOT NULL 
      AND deleted IS FALSE
  )
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
SELECT
  id,
  group_id,
  src_tool_urn,
  src_tool_name,
  confirm,
  confirm_prompt,
  name,
  summary,
  description,
  tags,
  summarizer,
  created_at,
  updated_at
FROM tool_variations
WHERE
  group_id = (SELECT id FROM global_group)
  AND src_tool_name = ANY(@tool_names::text[])
  AND deleted IS FALSE
  AND id NOT IN (
    SELECT DISTINCT predecessor_id 
    FROM tool_variations 
    WHERE predecessor_id IS NOT NULL 
      AND deleted IS FALSE
  );

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
