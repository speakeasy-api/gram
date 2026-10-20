-- Directory review decisions. Every rule that opens or supersedes an item is
-- one query with an optional source filter: the directory event handlers pass
-- the source an event touched, and the reconciliation sweep passes none, so
-- both apply the same rule. A source is a directory group or an attribute key
-- and value. A group is live when neither deleted nor workos_deleted is set;
-- an attribute value is live while a live directory user carries it, matched
-- the same way access evaluation matches it (attributes ->> key = value).
-- Nothing here opens an item for an organization without a review state row:
-- the sweep creates that row, and with it the baseline.

-- name: InsertDirectoryReviewOrgStateIfMissing :exec
INSERT INTO directory_review_org_states (organization_id, baseline_at)
VALUES (@organization_id, clock_timestamp())
ON CONFLICT (organization_id) DO NOTHING;

-- name: GetDirectoryReviewOrgState :one
SELECT organization_id, baseline_at, emails_disabled_at, last_emailed_at, reminders_sent
FROM directory_review_org_states
WHERE organization_id = @organization_id;

-- name: ListOrganizationsForDirectoryReviewSweep :many
-- Organizations with directory sync on, or with directory mappings left from
-- when it was, whose sources may need decisions.
SELECT om.id
FROM organization_metadata AS om
WHERE om.disabled_at IS NULL
  AND (
    om.scim_enabled IS TRUE
    OR EXISTS (
      SELECT 1
      FROM directory_role_mappings AS drm
      WHERE drm.organization_id = om.id
        AND drm.deleted IS FALSE
    )
  )
ORDER BY om.id;

-- name: OpenNewGroupItems :many
-- A live group with no mapping that Speakeasy first saw after the baseline.
-- Raised once per group: a group that has had a new_group item is no longer
-- new, whatever happened to the item.
INSERT INTO directory_review_items (organization_id, kind, directory_group_id, occurrence, state)
SELECT dg.organization_id, 'new_group', dg.id, 1, 'pending'
FROM directory_groups AS dg
JOIN directory_review_org_states AS st
  ON st.organization_id = dg.organization_id
WHERE dg.organization_id = @organization_id
  AND (sqlc.narg('directory_group_id')::uuid IS NULL OR dg.id = sqlc.narg('directory_group_id')::uuid)
  AND dg.deleted IS FALSE
  AND dg.workos_deleted IS FALSE
  AND dg.created_at > st.baseline_at
  AND NOT EXISTS (
    SELECT 1
    FROM directory_role_mappings AS drm
    WHERE drm.organization_id = dg.organization_id
      AND drm.directory_group_id = dg.id
      AND drm.deleted IS FALSE
  )
  AND NOT EXISTS (
    SELECT 1
    FROM directory_review_items AS i
    WHERE i.organization_id = dg.organization_id
      AND i.kind = 'new_group'
      AND i.directory_group_id = dg.id
  )
ON CONFLICT DO NOTHING
RETURNING id, organization_id, kind, directory_group_id, attribute_key, attribute_value, occurrence, state;

-- name: OpenMissingGroupItems :many
-- A deleted or tombstoned group that still has mappings. A later deletion of
-- the same group, after it came back, is a new occurrence; a deletion already
-- covered by an item (pending, removed or kept) is not raised again.
INSERT INTO directory_review_items (organization_id, kind, directory_group_id, occurrence, state)
SELECT
  dg.organization_id,
  'source_missing',
  dg.id,
  COALESCE(latest.occurrence, 0) + 1,
  'pending'
FROM directory_groups AS dg
JOIN directory_review_org_states AS st
  ON st.organization_id = dg.organization_id
LEFT JOIN LATERAL (
  SELECT i.occurrence, i.created_at
  FROM directory_review_items AS i
  WHERE i.organization_id = dg.organization_id
    AND i.kind = 'source_missing'
    AND i.directory_group_id = dg.id
  ORDER BY i.occurrence DESC
  LIMIT 1
) AS latest ON TRUE
WHERE dg.organization_id = @organization_id
  AND (sqlc.narg('directory_group_id')::uuid IS NULL OR dg.id = sqlc.narg('directory_group_id')::uuid)
  AND (dg.deleted OR dg.workos_deleted)
  AND EXISTS (
    SELECT 1
    FROM directory_role_mappings AS drm
    WHERE drm.organization_id = dg.organization_id
      AND drm.directory_group_id = dg.id
      AND drm.deleted IS FALSE
  )
  AND (
    latest.created_at IS NULL
    OR latest.created_at < GREATEST(dg.workos_deleted_at, dg.deleted_at)
  )
ON CONFLICT DO NOTHING
RETURNING id, organization_id, kind, directory_group_id, attribute_key, attribute_value, occurrence, state;

-- name: OpenMissingAttributeItems :many
-- A mapped attribute value no live directory user carries. The event handler
-- calls this when an event has just taken the value from its last holder,
-- with allow_recurrence set, so a value that returned and went again gets a
-- new occurrence. The sweep cannot tell when a value went, so it only opens
-- the first occurrence.
INSERT INTO directory_review_items (organization_id, kind, attribute_key, attribute_value, occurrence, state)
SELECT
  src.organization_id,
  'source_missing',
  src.attribute_key,
  src.attribute_value,
  COALESCE(latest.occurrence, 0) + 1,
  'pending'
FROM (
  SELECT DISTINCT drm.organization_id, drm.attribute_key, drm.attribute_value
  FROM directory_role_mappings AS drm
  WHERE drm.organization_id = @organization_id
    AND drm.deleted IS FALSE
    AND drm.attribute_key IS NOT NULL
    AND (sqlc.narg('attribute_key')::text IS NULL OR drm.attribute_key = sqlc.narg('attribute_key')::text)
    AND (sqlc.narg('attribute_value')::text IS NULL OR drm.attribute_value = sqlc.narg('attribute_value')::text)
) AS src
JOIN directory_review_org_states AS st
  ON st.organization_id = src.organization_id
LEFT JOIN LATERAL (
  SELECT i.occurrence
  FROM directory_review_items AS i
  WHERE i.organization_id = src.organization_id
    AND i.kind = 'source_missing'
    AND i.attribute_key = src.attribute_key
    AND i.attribute_value = src.attribute_value
  ORDER BY i.occurrence DESC
  LIMIT 1
) AS latest ON TRUE
WHERE NOT EXISTS (
    SELECT 1
    FROM directory_users AS du
    WHERE du.organization_id = src.organization_id
      AND du.deleted IS FALSE
      AND du.workos_deleted IS FALSE
      AND du.attributes ->> src.attribute_key = src.attribute_value
  )
  AND (latest.occurrence IS NULL OR sqlc.arg(allow_recurrence)::boolean)
ON CONFLICT DO NOTHING
RETURNING id, organization_id, kind, directory_group_id, attribute_key, attribute_value, occurrence, state;

-- name: OpenNewAttributeValueItem :many
-- A value of an attribute key that already has a mapping, when the key is not
-- opted out and the value itself has no mapping. The event handler calls this
-- only when the event gave the value to its first live holder. Raised once per
-- value.
INSERT INTO directory_review_items (organization_id, kind, attribute_key, attribute_value, occurrence, state)
SELECT st.organization_id, 'new_attribute_value', sqlc.arg(attribute_key)::text, sqlc.arg(attribute_value)::text, 1, 'pending'
FROM directory_review_org_states AS st
WHERE st.organization_id = @organization_id
  AND EXISTS (
    SELECT 1
    FROM directory_role_mappings AS drm
    WHERE drm.organization_id = st.organization_id
      AND drm.deleted IS FALSE
      AND drm.attribute_key = sqlc.arg(attribute_key)::text
  )
  AND NOT EXISTS (
    SELECT 1
    FROM directory_role_mappings AS drm
    WHERE drm.organization_id = st.organization_id
      AND drm.deleted IS FALSE
      AND drm.attribute_key = sqlc.arg(attribute_key)::text
      AND drm.attribute_value = sqlc.arg(attribute_value)::text
  )
  AND NOT EXISTS (
    SELECT 1
    FROM directory_review_attribute_opt_outs AS o
    WHERE o.organization_id = st.organization_id
      AND o.attribute_key = sqlc.arg(attribute_key)::text
      AND o.deleted IS FALSE
  )
  AND EXISTS (
    SELECT 1
    FROM directory_users AS du
    WHERE du.organization_id = st.organization_id
      AND du.deleted IS FALSE
      AND du.workos_deleted IS FALSE
      AND du.attributes ->> sqlc.arg(attribute_key)::text = sqlc.arg(attribute_value)::text
  )
  AND NOT EXISTS (
    SELECT 1
    FROM directory_review_items AS i
    WHERE i.organization_id = st.organization_id
      AND i.kind = 'new_attribute_value'
      AND i.attribute_key = sqlc.arg(attribute_key)::text
      AND i.attribute_value = sqlc.arg(attribute_value)::text
  )
ON CONFLICT DO NOTHING
RETURNING id, organization_id, kind, directory_group_id, attribute_key, attribute_value, occurrence, state;

-- name: CountOtherLiveAttributeHolders :one
-- Live directory users other than the given one that carry an attribute value.
-- Read before an event writes a user, to tell whether the event is the value's
-- first appearance or takes it from its last holder.
SELECT COUNT(*)::bigint
FROM directory_users AS du
WHERE du.organization_id = @organization_id
  AND du.deleted IS FALSE
  AND du.workos_deleted IS FALSE
  AND du.workos_directory_user_id <> @workos_directory_user_id
  AND du.attributes ->> sqlc.arg(attribute_key)::text = sqlc.arg(attribute_value)::text;

-- name: GetDirectoryGroupIDForReview :one
-- The organization's group a directory event touched, deleted or not.
SELECT id
FROM directory_groups
WHERE organization_id = @organization_id
  AND workos_directory_group_id = @workos_directory_group_id;

-- name: GetDirectoryUserForReview :one
-- The organization's directory user a directory event touched, deleted or not.
SELECT deleted, workos_deleted
FROM directory_users
WHERE organization_id = @organization_id
  AND workos_directory_user_id = @workos_directory_user_id;

-- name: ListDirectoryUserAttributesForReview :many
-- A directory user's attribute values as the text ->> returns, which is what
-- mappings match on. JSON nulls are left out because ->> makes them NULL.
SELECT a.key::text AS attribute_key, a.value::text AS attribute_value
FROM directory_users AS du
CROSS JOIN LATERAL jsonb_each_text(du.attributes) AS a
WHERE du.organization_id = @organization_id
  AND du.workos_directory_user_id = @workos_directory_user_id
  AND jsonb_typeof(du.attributes) = 'object'
  AND a.value IS NOT NULL;

-- name: ListMappedAttributeKeys :many
-- Attribute keys with at least one live mapping, the only keys whose value
-- changes can raise or settle a decision.
SELECT DISTINCT attribute_key::text
FROM directory_role_mappings
WHERE organization_id = @organization_id
  AND deleted IS FALSE
  AND attribute_key IS NOT NULL
ORDER BY 1;

-- name: SupersedeDirectoryReviewItems :many
-- Settles pending items whose condition no longer holds. With no source
-- filter it covers the whole organization, as the sweep does:
--   new_group: the group got a mapping or is gone.
--   new_attribute_value: the value got a mapping, nobody carries it, its key
--     has no mapping left, or its key was opted out.
--   source_missing: the source is live again, or it has no mapping left.
UPDATE directory_review_items AS i
SET state = 'superseded',
    resolved_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE i.organization_id = @organization_id
  AND i.state = 'pending'
  AND (sqlc.narg('directory_group_id')::uuid IS NULL OR i.directory_group_id = sqlc.narg('directory_group_id')::uuid)
  AND (sqlc.narg('attribute_key')::text IS NULL OR i.attribute_key = sqlc.narg('attribute_key')::text)
  AND (sqlc.narg('attribute_value')::text IS NULL OR i.attribute_value = sqlc.narg('attribute_value')::text)
  AND (
    CASE
      WHEN i.directory_group_id IS NOT NULL THEN
        CASE i.kind
          WHEN 'new_group' THEN
            EXISTS (
              SELECT 1 FROM directory_role_mappings AS drm
              WHERE drm.organization_id = i.organization_id
                AND drm.directory_group_id = i.directory_group_id
                AND drm.deleted IS FALSE
            )
            OR NOT EXISTS (
              SELECT 1 FROM directory_groups AS dg
              WHERE dg.organization_id = i.organization_id
                AND dg.id = i.directory_group_id
                AND dg.deleted IS FALSE
                AND dg.workos_deleted IS FALSE
            )
          WHEN 'source_missing' THEN
            EXISTS (
              SELECT 1 FROM directory_groups AS dg
              WHERE dg.organization_id = i.organization_id
                AND dg.id = i.directory_group_id
                AND dg.deleted IS FALSE
                AND dg.workos_deleted IS FALSE
            )
            OR NOT EXISTS (
              SELECT 1 FROM directory_role_mappings AS drm
              WHERE drm.organization_id = i.organization_id
                AND drm.directory_group_id = i.directory_group_id
                AND drm.deleted IS FALSE
            )
          ELSE FALSE
        END
      ELSE
        CASE i.kind
          WHEN 'new_attribute_value' THEN
            EXISTS (
              SELECT 1 FROM directory_role_mappings AS drm
              WHERE drm.organization_id = i.organization_id
                AND drm.attribute_key = i.attribute_key
                AND drm.attribute_value = i.attribute_value
                AND drm.deleted IS FALSE
            )
            OR NOT EXISTS (
              SELECT 1 FROM directory_role_mappings AS drm
              WHERE drm.organization_id = i.organization_id
                AND drm.attribute_key = i.attribute_key
                AND drm.deleted IS FALSE
            )
            OR EXISTS (
              SELECT 1 FROM directory_review_attribute_opt_outs AS o
              WHERE o.organization_id = i.organization_id
                AND o.attribute_key = i.attribute_key
                AND o.deleted IS FALSE
            )
            OR NOT EXISTS (
              SELECT 1 FROM directory_users AS du
              WHERE du.organization_id = i.organization_id
                AND du.deleted IS FALSE
                AND du.workos_deleted IS FALSE
                AND du.attributes ->> i.attribute_key = i.attribute_value
            )
          WHEN 'source_missing' THEN
            EXISTS (
              SELECT 1 FROM directory_users AS du
              WHERE du.organization_id = i.organization_id
                AND du.deleted IS FALSE
                AND du.workos_deleted IS FALSE
                AND du.attributes ->> i.attribute_key = i.attribute_value
            )
            OR NOT EXISTS (
              SELECT 1 FROM directory_role_mappings AS drm
              WHERE drm.organization_id = i.organization_id
                AND drm.attribute_key = i.attribute_key
                AND drm.attribute_value = i.attribute_value
                AND drm.deleted IS FALSE
            )
          ELSE FALSE
        END
    END
  )
RETURNING i.id, i.organization_id, i.kind, i.directory_group_id, i.attribute_key, i.attribute_value, i.occurrence, i.state;

-- name: ListPendingDirectoryReviewItems :many
-- An organization's pending items, oldest first, with the directory people
-- each source matches now, counted as bulk setup counts them: distinct
-- emails of live directory users.
SELECT
  i.id,
  i.kind,
  i.directory_group_id,
  dg.name AS directory_group_name,
  i.attribute_key,
  i.attribute_value,
  i.occurrence,
  i.created_at,
  CASE
    WHEN i.directory_group_id IS NOT NULL THEN (
      SELECT COUNT(DISTINCT NULLIF(LOWER(TRIM(du.email)), ''))
      FROM directory_user_group_memberships AS m
      JOIN directory_users AS du
        ON du.id = m.directory_user_id
        AND du.organization_id = i.organization_id
        AND du.deleted IS FALSE
        AND du.workos_deleted IS FALSE
      WHERE m.directory_group_id = i.directory_group_id
        AND m.deleted IS FALSE
        AND dg.deleted IS FALSE
        AND dg.workos_deleted IS FALSE
    )
    ELSE (
      SELECT COUNT(DISTINCT NULLIF(LOWER(TRIM(du.email)), ''))
      FROM directory_users AS du
      WHERE du.organization_id = i.organization_id
        AND du.deleted IS FALSE
        AND du.workos_deleted IS FALSE
        AND du.attributes ->> i.attribute_key = i.attribute_value
    )
  END::bigint AS directory_people
FROM directory_review_items AS i
LEFT JOIN directory_groups AS dg
  ON dg.id = i.directory_group_id
  AND dg.organization_id = i.organization_id
WHERE i.organization_id = @organization_id
  AND i.state = 'pending'
ORDER BY i.created_at, i.id;

-- name: GetDirectoryReviewItem :one
-- An item of the organization, in any state.
SELECT id, organization_id, kind, directory_group_id, attribute_key, attribute_value, occurrence, state
FROM directory_review_items
WHERE organization_id = @organization_id
  AND id = @id;

-- name: GetPendingDirectoryReviewItemForUpdate :one
-- Locks a pending item so two admins cannot resolve it at once.
SELECT id, organization_id, kind, directory_group_id, attribute_key, attribute_value, occurrence, state
FROM directory_review_items
WHERE organization_id = @organization_id
  AND id = @id
  AND state = 'pending'
FOR UPDATE;

-- name: ResolveDirectoryReviewItem :one
UPDATE directory_review_items
SET state = sqlc.arg(state)::text,
    resolved_at = clock_timestamp(),
    resolved_by_user_id = sqlc.narg('resolved_by_user_id')::text,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND id = @id
  AND state = 'pending'
RETURNING id, organization_id, kind, directory_group_id, attribute_key, attribute_value, occurrence, state;

-- name: InsertDirectoryReviewAttributeOptOut :exec
INSERT INTO directory_review_attribute_opt_outs (organization_id, attribute_key)
VALUES (@organization_id, @attribute_key)
ON CONFLICT DO NOTHING;

-- name: CountPendingDirectoryReviewItems :one
SELECT COUNT(*)::bigint
FROM directory_review_items
WHERE organization_id = @organization_id
  AND state = 'pending';

-- name: ListDirectoryReviewEmailCandidates :many
-- Organizations with pending items and review emails on, with what the email
-- step needs to decide whether to announce, remind or wait.
SELECT
  s.organization_id,
  o.name AS organization_name,
  o.slug AS organization_slug,
  o.default_host,
  s.last_emailed_at,
  s.reminders_sent,
  COUNT(*) FILTER (WHERE i.kind = 'new_group')::bigint AS new_group_count,
  COUNT(*) FILTER (WHERE i.kind = 'new_attribute_value')::bigint AS new_attribute_value_count,
  COUNT(*) FILTER (WHERE i.kind = 'source_missing')::bigint AS source_missing_count,
  COUNT(*) FILTER (WHERE i.notified_at IS NULL)::bigint AS unannounced_count,
  MAX(i.created_at) FILTER (WHERE i.notified_at IS NULL)::timestamptz AS newest_unannounced_at,
  COALESCE(array_agg(i.id) FILTER (WHERE i.notified_at IS NULL), '{}')::uuid[] AS unannounced_ids
FROM directory_review_org_states AS s
JOIN organization_metadata AS o ON o.id = s.organization_id
JOIN directory_review_items AS i ON i.organization_id = s.organization_id AND i.state = 'pending'
WHERE s.emails_disabled_at IS NULL
GROUP BY s.organization_id, o.name, o.slug, o.default_host, s.last_emailed_at, s.reminders_sent
ORDER BY s.organization_id;

-- name: MarkDirectoryReviewItemsAnnounced :exec
-- Marks exactly the items an announcement counted, so an item committed
-- after the count is announced by the next email instead.
UPDATE directory_review_items
SET notified_at = @notified_at, updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND notified_at IS NULL
  AND id = ANY(@ids::uuid[]);

-- name: RecordDirectoryReviewAnnouncement :exec
-- An announcement restarts the reminder count.
UPDATE directory_review_org_states
SET last_emailed_at = @emailed_at, reminders_sent = 0, updated_at = clock_timestamp()
WHERE organization_id = @organization_id;

-- name: RecordDirectoryReviewReminder :execrows
-- Counts a reminder, unless another run already counted it.
UPDATE directory_review_org_states
SET last_emailed_at = @emailed_at, reminders_sent = reminders_sent + 1, updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND reminders_sent = @reminders_sent;

-- name: LockDirectoryReviewSettings :exec
-- Serializes changes to one organization's directory review settings for the
-- rest of the transaction.
SELECT pg_advisory_xact_lock(hashtextextended('directory_review_settings:' || @organization_id::text, 0));

-- name: GetDirectoryReviewEmailsDisabledAt :one
-- When the organization turned directory review emails off. No row, or a NULL
-- time, means emails are on.
SELECT emails_disabled_at
FROM directory_review_org_states
WHERE organization_id = @organization_id;

-- name: SetDirectoryReviewEmailsDisabled :one
-- Turns directory review emails off or on. Creates the organization's review
-- state if the sweep has not yet, with the baseline it would record.
INSERT INTO directory_review_org_states (organization_id, baseline_at, emails_disabled_at)
VALUES (@organization_id, clock_timestamp(), CASE WHEN @disabled::boolean THEN clock_timestamp() END)
ON CONFLICT (organization_id) DO UPDATE
SET emails_disabled_at = CASE
      WHEN @disabled::boolean THEN COALESCE(directory_review_org_states.emails_disabled_at, clock_timestamp())
    END,
    updated_at = clock_timestamp()
RETURNING emails_disabled_at;
