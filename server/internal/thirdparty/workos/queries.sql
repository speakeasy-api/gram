-- name: GetOrganizationSyncLastEventID :one
SELECT last_event_id
FROM workos_organization_syncs
WHERE workos_organization_id = @workos_organization_id;

-- name: SetOrganizationSyncLastEventID :one
INSERT INTO workos_organization_syncs (workos_organization_id, last_event_id)
VALUES (@workos_organization_id, @last_event_id)
ON CONFLICT (workos_organization_id) DO UPDATE SET
    last_event_id = EXCLUDED.last_event_id,
    updated_at = clock_timestamp()
RETURNING id;

-- name: ListActiveDirectoryGroupIDsByEmails :many
SELECT DISTINCT
  LOWER(du.email) AS email,
  dg.id AS directory_group_id
FROM directory_users AS du
JOIN directory_user_group_memberships AS m
  ON m.directory_user_id = du.id
  AND m.deleted IS FALSE
JOIN directory_groups AS dg
  ON dg.id = m.directory_group_id
  AND dg.organization_id = du.organization_id
  AND dg.deleted IS FALSE
  AND dg.workos_deleted IS FALSE
WHERE du.organization_id = @organization_id
  AND du.deleted IS FALSE
  AND du.workos_deleted IS FALSE
  AND du.email IS NOT NULL
  AND LOWER(du.email) = ANY(@emails::text[])
ORDER BY email, directory_group_id;

-- name: ListActiveDirectoryUserAttributesByEmails :many
SELECT DISTINCT
  LOWER(du.email) AS email,
  attribute.key::text AS attribute_key,
  attribute.value::text AS attribute_value
FROM directory_users AS du
CROSS JOIN LATERAL jsonb_each_text(
  CASE jsonb_typeof(du.attributes)
    WHEN 'object' THEN du.attributes
    ELSE '{}'::jsonb
  END
) AS attribute(key, value)
WHERE du.organization_id = @organization_id
  AND du.deleted IS FALSE
  AND du.workos_deleted IS FALSE
  AND du.email IS NOT NULL
  AND LOWER(du.email) = ANY(@emails::text[])
  AND attribute.value IS NOT NULL
ORDER BY email, attribute.key, attribute.value;

-- name: DirectoryAttributeValueExists :one
SELECT EXISTS(
  SELECT 1
  FROM directory_users AS du
  WHERE du.organization_id = @organization_id
    AND du.deleted IS FALSE
    AND du.workos_deleted IS FALSE
    AND du.attributes ->> @attribute_key = @attribute_value
);

-- name: DirectoryGroupExists :one
SELECT EXISTS(
  SELECT 1
  FROM directory_groups
  WHERE id = @id
    AND organization_id = @organization_id
    AND deleted IS FALSE
    AND workos_deleted IS FALSE
);

-- name: ListActiveDirectoryGroups :many
SELECT
  dg.id,
  dg.name,
  COUNT(DISTINCT NULLIF(LOWER(TRIM(du.email)), ''))::bigint AS member_count
FROM directory_groups AS dg
LEFT JOIN directory_user_group_memberships AS m
  ON m.directory_group_id = dg.id
  AND m.deleted IS FALSE
LEFT JOIN directory_users AS du
  ON du.id = m.directory_user_id
  AND du.organization_id = dg.organization_id
  AND du.deleted IS FALSE
  AND du.workos_deleted IS FALSE
WHERE dg.organization_id = @organization_id
  AND dg.deleted IS FALSE
  AND dg.workos_deleted IS FALSE
GROUP BY dg.id, dg.name
ORDER BY dg.name, dg.id;

-- name: ListActiveDirectoryAttributeValues :many
SELECT
  attribute.key::text AS attribute_key,
  attribute.value::text AS attribute_value,
  COUNT(DISTINCT NULLIF(LOWER(TRIM(du.email)), ''))::bigint AS member_count
FROM directory_users AS du
CROSS JOIN LATERAL jsonb_each_text(
  CASE jsonb_typeof(du.attributes)
    WHEN 'object' THEN du.attributes
    ELSE '{}'::jsonb
  END
) AS attribute(key, value)
WHERE du.organization_id = @organization_id
  AND du.deleted IS FALSE
  AND du.workos_deleted IS FALSE
  AND attribute.value IS NOT NULL
GROUP BY attribute.key, attribute.value
ORDER BY attribute.key, attribute.value;

-- name: GetUserSyncLastEventID :one
SELECT last_event_id
FROM workos_user_syncs
WHERE workos_user_id = @workos_user_id;

-- name: SetUserSyncLastEventID :one
INSERT INTO workos_user_syncs (workos_user_id, last_event_id)
VALUES (@workos_user_id, @last_event_id)
ON CONFLICT (workos_user_id) WHERE workos_user_id IS NOT NULL DO UPDATE SET
    last_event_id = EXCLUDED.last_event_id,
    updated_at = clock_timestamp()
RETURNING id;
