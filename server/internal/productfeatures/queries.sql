-- name: ListProOrganizations :many
SELECT id
FROM organization_metadata
WHERE gram_account_type = 'pro'
ORDER BY id;

-- name: LockAndCheckProOrganization :one
SELECT gram_account_type = 'pro' AS is_pro
FROM organization_metadata
WHERE id = @organization_id
FOR UPDATE;

-- name: LockOrganizationMetadata :one
SELECT id
FROM organization_metadata
WHERE id = @organization_id
FOR UPDATE;

-- name: AcquireFeatureCacheLock :exec
-- Serialize durable feature updates with cache fills and refreshes so an older
-- operation cannot overwrite a newer cache value after the database changes.
SELECT pg_advisory_lock(hashtextextended('product-feature:' || @organization_id::text || ':' || @feature_name::text, 0));

-- name: ReleaseFeatureCacheLock :one
SELECT pg_advisory_unlock(hashtextextended('product-feature:' || @organization_id::text || ':' || @feature_name::text, 0)) AS unlocked;

-- name: IsFeatureEnabled :one
-- The inverted gateway setting defaults to disabled until a soft-deleted
-- flag records an administrator's explicit opt-in.
SELECT COALESCE(EXISTS (
    SELECT 1 FROM organization_features AS active_feature
    WHERE active_feature.organization_id = @organization_id
      AND active_feature.feature_name = @feature_name
      AND active_feature.deleted IS FALSE
) OR (
    @feature_name::text = 'organization_gateway_disabled'
    AND NOT EXISTS (
        SELECT 1 FROM organization_features AS configured_feature
        WHERE configured_feature.organization_id = @organization_id
          AND configured_feature.feature_name = @feature_name
    )
), FALSE)::boolean AS enabled;

-- name: HasDeviceAgentSync :one
-- Whether any device has polled agent.getPlugins for the org — the member-
-- readable "org uses the device agent" signal (device_agent_syncs is written
-- only by the device-agent poll path).
SELECT EXISTS (
        SELECT 1
        FROM device_agent_syncs
        WHERE organization_id = @organization_id
) AS has_sync;

-- name: EnableFeature :execrows
INSERT INTO organization_features (
    organization_id,
    feature_name
) VALUES (
    @organization_id,
    @feature_name
)
ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE
DO NOTHING;

-- name: EnableFeatureIfNeverConfigured :execrows
-- Paid-tier activation grants enterprise-access capabilities while preserving
-- a soft-deleted row as an explicit administrator choice.
INSERT INTO organization_features (
    organization_id,
    feature_name
)
SELECT @organization_id, @feature_name
WHERE NOT EXISTS (
    SELECT 1
    FROM organization_features
    WHERE organization_id = @organization_id
      AND feature_name = @feature_name
)
ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE
DO NOTHING;

-- name: DeleteFeature :one
UPDATE organization_features
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND feature_name = @feature_name
  AND deleted IS FALSE
RETURNING *;
