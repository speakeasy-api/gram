-- name: IsFeatureEnabled :one
SELECT EXISTS (
        SELECT 1
        FROM organization_features
        WHERE organization_id = @organization_id
            AND feature_name = @feature_name
            AND deleted IS FALSE
) AS enabled;

-- name: HasDeviceAgentSync :one
-- Whether any device has polled agent.getPlugins for the org — the member-
-- readable "org uses the device agent" signal (device_agent_syncs is written
-- only by the device-agent poll path).
SELECT EXISTS (
        SELECT 1
        FROM device_agent_syncs
        WHERE organization_id = @organization_id
) AS has_sync;

-- name: EnableFeature :exec
INSERT INTO organization_features (
    organization_id,
    feature_name
) VALUES (
    @organization_id,
    @feature_name
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
