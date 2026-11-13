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
