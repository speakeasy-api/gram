-- Queries behind the Speakeasy Slack app's Home tab. Every query is bounded by
-- organization_id: the Home tab shows a linked person their organization's
-- data across projects. Permission filters run in the query, before any
-- LIMIT, so a capped list never hides rows the viewer may see.

-- name: GetHomeOrganization :one
SELECT name, slug
FROM organization_metadata
WHERE id = @organization_id;

-- name: HasHomeApproverPolicy :one
-- Whether any of @principal_urns is an approver on a live require_approval
-- policy in the organization, so the approvals section shows to approvers
-- even when nothing is waiting.
SELECT EXISTS (
  SELECT 1
  FROM risk_policies rp
  JOIN projects p
    ON p.id = rp.project_id
    AND p.organization_id = rp.organization_id
    AND p.deleted IS FALSE
  WHERE rp.organization_id = @organization_id
    AND rp.deleted IS FALSE
    AND rp.enabled IS TRUE
    AND rp.action = 'require_approval'
    AND jsonb_exists_any(rp.approval_config -> 'approvers', @principal_urns::text[])
);

-- name: ListHomePendingApprovals :many
-- Open tool approval requests across the organization's projects whose
-- policy names one of @principal_urns as an approver, newest first.
SELECT
  a.id
  , a.project_id
  , a.tool_name
  , a.requester_urn
  , a.requester_user_id
  , rp.name AS policy_name
  , p.name AS project_name
  , p.slug AS project_slug
  , COALESCE(NULLIF(u.display_name, ''), u.email, '')::text AS requester_name
FROM risk_policy_approvals a
JOIN projects p
  ON p.id = a.project_id
  AND p.organization_id = a.organization_id
  AND p.deleted IS FALSE
JOIN risk_policies rp
  ON rp.id = a.risk_policy_id
  AND rp.project_id = a.project_id
  AND rp.organization_id = a.organization_id
  AND rp.deleted IS FALSE
LEFT JOIN users u ON u.id = a.requester_user_id
WHERE a.organization_id = @organization_id
  AND a.deleted IS FALSE
  AND a.status = 'pending'
  AND (a.expires_at IS NULL OR a.expires_at > clock_timestamp())
  AND jsonb_exists_any(rp.approval_config -> 'approvers', @principal_urns::text[])
ORDER BY a.id DESC
LIMIT @max_rows;

-- name: ListHomeMCPRequests :many
-- MCP server reviews waiting on a decision across the organization's
-- projects, most recently updated first.
SELECT
  r.id
  , r.project_id
  , r.target_raw
  , p.name AS project_name
  , p.slug AS project_slug
FROM mcp_approval_requests r
JOIN projects p
  ON p.id = r.project_id
  AND p.organization_id = r.organization_id
  AND p.deleted IS FALSE
WHERE r.organization_id = @organization_id
  AND r.deleted IS FALSE
  AND r.status = 'requested'
ORDER BY r.updated_at DESC
LIMIT @max_rows;

-- name: ListHomeHighRiskFindings :many
-- The newest open high or critical findings across the organization's
-- projects. Severity follows risk.FindingSeverity: a live policy's score
-- rounded to one decimal, >= 7.0; a finding whose policy is deleted or
-- unscored reads as its category's fallback band. @category_rules lists the
-- categories in risk/categories.Classify order as
-- {ord, source, prefix, rule_ids, high}; the first match decides, and
-- @custom_high is the band of a finding no category matches. The severity
-- filter runs before both LIMITs, so older high findings are never crowded
-- out by newer low ones.
SELECT
  f.id
  , f.project_id
  , f.created_at
  , f.policy_name
  , f.policy_score
  , f.policy_live
  , f.source
  , f.rule_id
  , p.name AS project_name
  , p.slug AS project_slug
FROM projects p
CROSS JOIN LATERAL (
  SELECT
    r.id
    , r.project_id
    , r.created_at
    , r.source
    , COALESCE(r.rule_id, '')::text AS rule_id
    , rp.name AS policy_name
    , rp.score AS policy_score
    , (rp.deleted IS FALSE AND rp.score > 0) AS policy_live
  FROM risk_results r
  JOIN risk_policies rp
    ON rp.id = r.risk_policy_id
    AND rp.project_id = r.project_id
  WHERE r.project_id = p.id
    AND r.organization_id = p.organization_id
    AND r.found IS TRUE
    AND r.excluded_at IS NULL
    AND r.false_positive_at IS NULL
    AND CASE
      WHEN rp.deleted IS FALSE AND rp.score > 0 THEN round(rp.score::numeric, 1) >= 7.0
      ELSE COALESCE((
        SELECT c.high
        FROM jsonb_to_recordset(@category_rules::jsonb) AS c(ord int, source text, prefix text, rule_ids text[], high boolean)
        WHERE (c.source <> '' AND c.source = r.source)
          OR (
            COALESCE(r.rule_id, '') <> ''
            AND (r.rule_id = ANY(c.rule_ids) OR (c.prefix <> '' AND starts_with(r.rule_id, c.prefix)))
          )
        ORDER BY c.ord
        LIMIT 1
      ), @custom_high::boolean)
    END
  ORDER BY r.created_at DESC
  LIMIT @max_rows
) f
WHERE p.organization_id = @organization_id
  AND p.deleted IS FALSE
ORDER BY f.created_at DESC
LIMIT @max_rows;

-- name: ListHomeServers :many
-- Every enabled MCP server in the organization, most recently updated first.
-- Built-in backends Speakeasy serves itself are left out. Not limited: the
-- caller keeps the ones the viewer can read and caps the list after that.
SELECT
  s.id
  , s.project_id
  , s.toolset_id
  , COALESCE(s.slug, '')::text AS slug
  , COALESCE(s.name, t.name, s.slug, '')::text AS name
  , logo.id AS logo_id
  , p.name AS project_name
  , p.slug AS project_slug
FROM mcp_servers s
JOIN projects p
  ON p.id = s.project_id
  AND p.deleted IS FALSE
LEFT JOIN toolsets t
  ON t.id = s.toolset_id
  AND t.project_id = s.project_id
  AND t.deleted IS FALSE
LEFT JOIN mcp_metadata metadata
  ON metadata.mcp_server_id = s.id
  AND metadata.project_id = s.project_id
LEFT JOIN assets logo
  ON logo.id = metadata.logo_id
  AND (logo.project_id = s.project_id
    OR (logo.project_id IS NULL AND (logo.organization_id = p.organization_id OR logo.organization_id IS NULL)))
  AND logo.kind = 'image'
  AND logo.content_type IN ('image/png', 'image/jpeg', 'image/gif')
WHERE p.organization_id = @organization_id
  AND s.deleted IS FALSE
  AND s.visibility <> 'disabled'
  AND s.builtin_backend IS NULL
ORDER BY s.updated_at DESC;
