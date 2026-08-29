-- PERFORMANCE NOTE: Any pull request that modifies this query must include query-performance
-- evidence in its description, including EXPLAIN (ANALYZE, BUFFERS) output at the configured
-- maximum subscriber batch size. Profile only against a local database seeded with
-- production-like counts for chats, users, directory profiles, groups, memberships, and roles;
-- include both direct-user and email-fallback matches. Run ANALYZE after seeding and warm the
-- query once before recording repeated measurements.
-- name: ResolveAgentSessionStorageAttributes :many
WITH inputs AS (
  SELECT
      input.ordinality
    , input.organization_id
    , (@project_ids::uuid[])[input.ordinality] AS project_id
    , (@chat_ids::uuid[])[input.ordinality] AS chat_id
    , NULLIF((@message_user_ids::text[])[input.ordinality], '') AS message_user_id
  FROM UNNEST(@organization_ids::text[]) WITH ORDINALITY AS input(organization_id, ordinality)
), scoped_chats AS (
  SELECT
      i.ordinality
    , i.organization_id
    , i.project_id
    , i.chat_id
    , i.message_user_id
    , c.user_id AS chat_owner_user_id
    , c.external_user_id AS chat_owner_external_user_id
  FROM inputs i
  INNER JOIN projects p
    ON p.id = i.project_id
    AND p.organization_id = i.organization_id
    AND p.deleted IS FALSE
  INNER JOIN chats c
    ON c.id = i.chat_id
    AND c.project_id = i.project_id
    AND c.organization_id = i.organization_id
    AND c.deleted IS FALSE
), principal_refs AS (
  SELECT
      scoped.*
    , 'message_user'::text AS principal_kind
    , scoped.message_user_id AS principal_user_id
  FROM scoped_chats scoped
  UNION ALL
  SELECT
      scoped.*
    , 'chat_owner'::text AS principal_kind
    , scoped.chat_owner_user_id AS principal_user_id
  FROM scoped_chats scoped
)
SELECT
    refs.ordinality::bigint AS ordinality
  , refs.organization_id::text AS organization_id
  , refs.project_id::uuid AS project_id
  , refs.chat_id::uuid AS chat_id
  , COALESCE(refs.message_user_id, '')::text AS message_user_id
  , COALESCE(MAX(identity.account_email) FILTER (WHERE refs.principal_kind = 'message_user'), '')::text AS message_user_account_email
  , COALESCE(MAX(identity.division_name) FILTER (WHERE refs.principal_kind = 'message_user'), '')::text AS message_user_division_name
  , COALESCE(MAX(identity.department_name) FILTER (WHERE refs.principal_kind = 'message_user'), '')::text AS message_user_department_name
  , COALESCE(MAX(identity.job_title) FILTER (WHERE refs.principal_kind = 'message_user'), '')::text AS message_user_job_title
  , COALESCE(MAX(identity.employee_type) FILTER (WHERE refs.principal_kind = 'message_user'), '')::text AS message_user_employee_type
  , COALESCE(MAX(identity.cost_center_name) FILTER (WHERE refs.principal_kind = 'message_user'), '')::text AS message_user_cost_center_name
  , COALESCE(MAX(identity.group_names::text) FILTER (WHERE refs.principal_kind = 'message_user'), '{}')::text[] AS message_user_group_names
  , COALESCE(MAX(identity.directory_match) FILTER (WHERE refs.principal_kind = 'message_user'), '')::text AS message_user_directory_match
  , COALESCE(MAX(identity.role_slugs::text) FILTER (WHERE refs.principal_kind = 'message_user'), '{}')::text[] AS message_user_role_slugs
  , COALESCE(refs.chat_owner_user_id, '')::text AS chat_owner_user_id
  , COALESCE(refs.chat_owner_external_user_id, '')::text AS chat_owner_external_user_id
  , COALESCE(MAX(identity.account_email) FILTER (WHERE refs.principal_kind = 'chat_owner'), '')::text AS chat_owner_user_email
  , COALESCE(MAX(identity.division_name) FILTER (WHERE refs.principal_kind = 'chat_owner'), '')::text AS chat_owner_division_name
  , COALESCE(MAX(identity.department_name) FILTER (WHERE refs.principal_kind = 'chat_owner'), '')::text AS chat_owner_department_name
  , COALESCE(MAX(identity.job_title) FILTER (WHERE refs.principal_kind = 'chat_owner'), '')::text AS chat_owner_job_title
  , COALESCE(MAX(identity.employee_type) FILTER (WHERE refs.principal_kind = 'chat_owner'), '')::text AS chat_owner_employee_type
  , COALESCE(MAX(identity.cost_center_name) FILTER (WHERE refs.principal_kind = 'chat_owner'), '')::text AS chat_owner_cost_center_name
  , COALESCE(MAX(identity.group_names::text) FILTER (WHERE refs.principal_kind = 'chat_owner'), '{}')::text[] AS chat_owner_group_names
  , COALESCE(MAX(identity.directory_match) FILTER (WHERE refs.principal_kind = 'chat_owner'), '')::text AS chat_owner_directory_match
  , COALESCE(MAX(identity.role_slugs::text) FILTER (WHERE refs.principal_kind = 'chat_owner'), '{}')::text[] AS chat_owner_role_slugs
FROM principal_refs refs
LEFT JOIN LATERAL (
  SELECT
      u.email AS account_email
    , NULLIF(BTRIM(CASE WHEN jsonb_typeof(directory_user.attributes -> 'division_name') = 'string' THEN directory_user.attributes ->> 'division_name' END), '') AS division_name
    , NULLIF(BTRIM(CASE WHEN jsonb_typeof(directory_user.attributes -> 'department_name') = 'string' THEN directory_user.attributes ->> 'department_name' END), '') AS department_name
    , NULLIF(BTRIM(CASE WHEN jsonb_typeof(directory_user.attributes -> 'job_title') = 'string' THEN directory_user.attributes ->> 'job_title' END), '') AS job_title
    , NULLIF(BTRIM(CASE WHEN jsonb_typeof(directory_user.attributes -> 'employee_type') = 'string' THEN directory_user.attributes ->> 'employee_type' END), '') AS employee_type
    , NULLIF(BTRIM(CASE WHEN jsonb_typeof(directory_user.attributes -> 'cost_center_name') = 'string' THEN directory_user.attributes ->> 'cost_center_name' END), '') AS cost_center_name
    , COALESCE(directory_groups.group_names, '{}'::text[])::text[] AS group_names
    , directory_user.match_method AS directory_match
    , COALESCE(role_slugs.role_slugs, '{}'::text[])::text[] AS role_slugs
  FROM organization_user_relationships membership
  INNER JOIN users u
    ON u.id = membership.user_id
    AND u.deleted_at IS NULL
  LEFT JOIN LATERAL (
    SELECT candidate.id, candidate.attributes, candidate.match_method
    FROM (
      SELECT
          d.attributes
        , 'user_id'::text AS match_method
        , 0 AS match_priority
        , d.workos_updated_at
        , d.updated_at
        , d.id
      FROM directory_users d
      WHERE d.organization_id = refs.organization_id
        AND d.user_id = u.id
        AND d.user_id IS NOT NULL
        AND d.deleted IS FALSE
        AND d.workos_deleted IS FALSE
      UNION ALL
      SELECT
          email_candidate.attributes
        , 'email'::text AS match_method
        , 1 AS match_priority
        , email_candidate.workos_updated_at
        , email_candidate.updated_at
        , email_candidate.id
      FROM (
        SELECT
            d.attributes
          , d.workos_updated_at
          , d.updated_at
          , d.id
          , COUNT(*) OVER () AS candidate_count
        FROM directory_users d
        WHERE d.organization_id = refs.organization_id
          AND LOWER(d.email) = LOWER(u.email)
          AND d.user_id IS NULL
          AND d.deleted IS FALSE
          AND d.workos_deleted IS FALSE
      ) email_candidate
      WHERE email_candidate.candidate_count = 1
    ) candidate
    ORDER BY candidate.match_priority, candidate.workos_updated_at DESC, candidate.updated_at DESC, candidate.id
    LIMIT 1
  ) directory_user ON TRUE
  LEFT JOIN LATERAL (
    SELECT ARRAY_AGG(DISTINCT dg.name ORDER BY dg.name) AS group_names
    FROM directory_user_group_memberships membership
    INNER JOIN directory_groups dg
      ON dg.id = membership.directory_group_id
      AND dg.organization_id = refs.organization_id
      AND dg.deleted IS FALSE
      AND dg.workos_deleted IS FALSE
    WHERE membership.directory_user_id = directory_user.id
      AND membership.deleted IS FALSE
  ) directory_groups ON TRUE
  LEFT JOIN LATERAL (
    SELECT ARRAY_AGG(DISTINCT active_role.role_slug ORDER BY active_role.role_slug) AS role_slugs
    FROM (
      SELECT COALESCE(organization_role.workos_slug, global_role.workos_slug) AS role_slug
      FROM (
        SELECT assignment.role_urn
        FROM organization_role_assignments assignment
        WHERE assignment.organization_id = refs.organization_id
          AND assignment.user_id = u.id
          AND assignment.user_id IS NOT NULL
          AND assignment.deleted_at IS NULL
        UNION
        SELECT assignment.role_urn
        FROM organization_role_assignments assignment
        WHERE assignment.organization_id = refs.organization_id
          AND u.workos_id IS NOT NULL
          AND assignment.workos_user_id = u.workos_id
          AND assignment.deleted_at IS NULL
      ) assignment
      LEFT JOIN organization_roles organization_role
        ON assignment.role_urn = 'role:organization:' || organization_role.id::text
        AND organization_role.organization_id = refs.organization_id
        AND organization_role.deleted IS FALSE
        AND organization_role.workos_deleted IS FALSE
      LEFT JOIN global_roles global_role
        ON assignment.role_urn = 'role:global:' || global_role.id::text
        AND global_role.deleted IS FALSE
        AND global_role.workos_deleted IS FALSE
    ) active_role
    WHERE active_role.role_slug IS NOT NULL
  ) role_slugs ON TRUE
  WHERE membership.organization_id = refs.organization_id
    AND membership.user_id = refs.principal_user_id
    AND membership.deleted IS FALSE
) identity ON TRUE
GROUP BY
    refs.ordinality
  , refs.organization_id
  , refs.project_id
  , refs.chat_id
  , refs.message_user_id
  , refs.chat_owner_user_id
  , refs.chat_owner_external_user_id
ORDER BY refs.ordinality;
