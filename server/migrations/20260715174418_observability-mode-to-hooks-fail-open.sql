-- Translate organizations still on the removed observability_mode feature to
-- the hooks_fail_open feature that supersedes it (DNO-497). Observability
-- mode was equivalent to fail-open plus not creating blocking policies, so
-- fail-open preserves the outage tolerance those orgs opted into while any
-- blocking policies they define now enforce. Runs after the feature's
-- removal from the API, so no new observability_mode rows can appear.
INSERT INTO organization_features (organization_id, feature_name)
SELECT organization_id, 'hooks_fail_open'
FROM organization_features
WHERE feature_name = 'observability_mode'
  AND deleted IS FALSE
ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE
DO NOTHING;

-- Retire the observability_mode rows themselves: the feature no longer exists
-- in the API enum, so a live row would only be dead state.
UPDATE organization_features
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE feature_name = 'observability_mode'
  AND deleted IS FALSE;
