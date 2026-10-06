-- The temporary flag identifies organizations outside the staff rollout, not
-- successful setup. Run before the separate stored-flag cleanup.
SELECT o.id
FROM organization_metadata AS o
WHERE o.disabled_at IS NULL
  AND NOT EXISTS (
    SELECT 1
    FROM organization_features AS f
    WHERE f.organization_id = o.id
      AND f.feature_name = 'automatic-role-distribution'
      AND f.deleted IS FALSE
  )
ORDER BY o.id;
