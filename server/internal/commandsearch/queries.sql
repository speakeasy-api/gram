-- Each query performs bounded server-side prefix matching and overfetches one
-- row beyond the per-kind response cap. Names use matching lower-expression
-- indexes; canonical lowercase slugs and rule ids use raw composite indexes.

-- name: ListCommandIndexEnvironments :many
SELECT id, name, slug FROM environments
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND (
    (lower(name) >= @lower_bound::text AND lower(name) < @lower_ceiling::text)
    OR (slug >= @lower_bound::text AND slug < @lower_ceiling::text)
  )
ORDER BY CASE
  WHEN slug = @lower_bound::text OR lower(name) = @lower_bound::text THEN 0
  WHEN slug >= @lower_bound::text AND slug < @lower_ceiling::text THEN 1
  ELSE 2
END, name, id
LIMIT @result_limit;

-- name: ListCommandIndexGateways :many
SELECT id, name FROM meta_mcp_servers
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
  AND lower(name) >= @lower_bound::text
  AND lower(name) < @lower_ceiling::text
ORDER BY CASE WHEN lower(name) = @lower_bound::text THEN 0 ELSE 2 END, name, id
LIMIT @result_limit;

-- name: ListCommandIndexPlugins :many
SELECT id, name, slug FROM plugins
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
  AND (
    (lower(name) >= @lower_bound::text AND lower(name) < @lower_ceiling::text)
    OR (slug >= @lower_bound::text AND slug < @lower_ceiling::text)
  )
ORDER BY CASE
  WHEN slug = @lower_bound::text OR lower(name) = @lower_bound::text THEN 0
  WHEN slug >= @lower_bound::text AND slug < @lower_ceiling::text THEN 1
  ELSE 2
END, name, id
LIMIT @result_limit;

-- name: ListCommandIndexCustomRules :many
SELECT id, title, rule_id, severity FROM risk_custom_detection_rules
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
  AND (
    (lower(title) >= @lower_bound::text AND lower(title) < @lower_ceiling::text)
    OR (rule_id >= @lower_bound::text AND rule_id < @lower_ceiling::text)
  )
ORDER BY CASE
  WHEN rule_id = @lower_bound::text OR lower(title) = @lower_bound::text THEN 0
  WHEN rule_id >= @lower_bound::text AND rule_id < @lower_ceiling::text THEN 1
  ELSE 2
END, title, id
LIMIT @result_limit;
-- name: ListCommandIndexAssistants :many
SELECT id, name FROM assistants
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND lower(name) >= @lower_bound::text
  AND lower(name) < @lower_ceiling::text
ORDER BY CASE WHEN lower(name) = @lower_bound::text THEN 0 ELSE 2 END, name, id
LIMIT @result_limit;

-- name: ListCommandIndexMCPServers :many
WITH candidates AS (
  SELECT ms.id, ms.name, ms.slug, ms.id AS route_id
  FROM mcp_servers ms
  WHERE ms.project_id = @project_id
    AND ms.deleted IS FALSE
    AND ms.toolset_id IS NULL
    AND ms.builtin_backend IS NULL
    AND num_nonnulls(ms.remote_mcp_server_id, ms.tunneled_mcp_server_id, ms.unproxied_mcp_server_id) > 0
    AND ((lower(ms.name) >= @lower_bound::text AND lower(ms.name) < @lower_ceiling::text)
      OR (ms.slug >= @lower_bound::text AND ms.slug < @lower_ceiling::text))
  UNION ALL
  SELECT t.id, t.name, t.slug, host.id AS route_id
  FROM toolsets t
  JOIN LATERAL (
    SELECT ms.id
    FROM mcp_servers ms
    WHERE ms.project_id = t.project_id
      AND ms.toolset_id = t.id
      AND ms.deleted IS FALSE
      AND ms.builtin_backend IS NULL
      AND (ms.id = t.id OR EXISTS (
        SELECT 1 FROM mcp_endpoints e
        WHERE e.project_id = ms.project_id AND e.mcp_server_id = ms.id AND e.deleted IS FALSE
      ))
    ORDER BY (ms.id = t.id) DESC, ms.created_at, ms.id
    LIMIT 1
  ) host ON TRUE
  WHERE t.project_id = @project_id
    AND t.deleted IS FALSE
    AND ((lower(t.name) >= @lower_bound::text AND lower(t.name) < @lower_ceiling::text)
      OR (t.slug >= @lower_bound::text AND t.slug < @lower_ceiling::text))
)
SELECT id, name, slug, route_id FROM candidates
ORDER BY CASE
  WHEN slug = @lower_bound::text OR lower(name) = @lower_bound::text THEN 0
  WHEN slug >= @lower_bound::text AND slug < @lower_ceiling::text THEN 1
  ELSE 2
END, name, id
LIMIT @result_limit;

-- name: ListCommandIndexSkills :many
SELECT id, display_name, name FROM skills
WHERE project_id = @project_id
  AND archived_at IS NULL
  AND ((lower(display_name) >= @lower_bound::text AND lower(display_name) < @lower_ceiling::text)
    OR (name >= @lower_bound::text AND name < @lower_ceiling::text))
ORDER BY CASE
  WHEN name = @lower_bound::text OR lower(display_name) = @lower_bound::text THEN 0
  WHEN name >= @lower_bound::text AND name < @lower_ceiling::text THEN 1
  ELSE 2
END, display_name, id
LIMIT @result_limit;
