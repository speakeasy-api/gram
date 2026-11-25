-- Modify "prompt_templates" table
ALTER TABLE "prompt_templates" DROP CONSTRAINT "prompt_templates_kind_check";

-- Lock every parent toolset row before selecting versions. Inserts into
-- toolset_versions take a key-share lock on the referenced parent, so this
-- serializes publication with concurrent toolset edits while still allowing
-- ordinary reads. Keep this as a separate statement so the publication query
-- receives a fresh snapshot after any in-flight writer finishes.
SELECT id
FROM toolsets
FOR UPDATE;

-- Publish a new version for each currently affected toolset. Toolset caches are
-- version-keyed, so this prevents a cached current version from advertising a
-- tool that the migration archives below.
WITH retired_tool_urns AS (
  SELECT COALESCE(ARRAY_AGG(DISTINCT tool_urn), ARRAY[]::TEXT[]) AS urns
  FROM prompt_templates
  WHERE kind = 'higher_order_tool'
    AND tool_urn IS NOT NULL
),
latest_versions AS (
  SELECT DISTINCT ON (tv.toolset_id) tv.*
  FROM toolset_versions tv
  WHERE tv.deleted IS FALSE
  ORDER BY tv.toolset_id, tv.version DESC
),
next_versions AS (
  SELECT toolset_id, MAX(version) + 1 AS version
  FROM toolset_versions
  GROUP BY toolset_id
),
latest_affected_versions AS (
  SELECT latest.*, next.version AS next_version, retired.urns
  FROM latest_versions latest
  JOIN next_versions next ON next.toolset_id = latest.toolset_id
  CROSS JOIN retired_tool_urns retired
  WHERE latest.tool_urns && retired.urns
)
INSERT INTO toolset_versions (
  toolset_id,
  version,
  tool_urns,
  resource_urns,
  predecessor_id
)
SELECT
  toolset_id,
  next_version,
  ARRAY(
    SELECT tool_urn
    FROM UNNEST(latest.tool_urns) AS tool_urn
    WHERE tool_urn != ALL(latest.urns)
  ),
  resource_urns,
  id
FROM latest_affected_versions latest;

-- Remove retired custom tools from historical versions before archiving their
-- backing templates, keeping every version free of dangling tool URNs.
WITH retired_tool_urns AS (
  SELECT COALESCE(ARRAY_AGG(DISTINCT tool_urn), ARRAY[]::TEXT[]) AS urns
  FROM prompt_templates
  WHERE kind = 'higher_order_tool'
    AND tool_urn IS NOT NULL
)
UPDATE toolset_versions tv
SET
  tool_urns = ARRAY(
    SELECT tool_urn
    FROM UNNEST(tv.tool_urns) AS tool_urn
    WHERE tool_urn != ALL(retired.urns)
  ),
  updated_at = clock_timestamp()
FROM retired_tool_urns retired
WHERE tv.tool_urns && retired.urns;

-- Higher-order tools were normally attached as tools, but detach any that were
-- also attached through the shared MCP prompt relationship.
DELETE FROM toolset_prompts tp
USING prompt_templates pt
WHERE pt.kind = 'higher_order_tool'
  AND pt.project_id = tp.project_id
  AND (
    pt.id = tp.prompt_template_id
    OR pt.history_id = tp.prompt_history_id
  );

UPDATE prompt_templates
SET
  deleted_at = clock_timestamp(),
  updated_at = clock_timestamp()
WHERE kind = 'higher_order_tool'
  AND deleted IS FALSE;
