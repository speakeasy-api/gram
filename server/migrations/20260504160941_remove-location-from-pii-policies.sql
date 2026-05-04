-- Remove the LOCATION Presidio entity from existing risk policies. The
-- geographically defined LOCATION entity is being retired from the Policy
-- Center because Presidio's NER-backed location detection produces too
-- many false positives on common nouns to be useful in the runtime hot
-- path. The selectable rule was removed from the dashboard in a separate
-- change; this migration cleans up policies that already had it pinned.
UPDATE risk_policies
SET presidio_entities = array_remove(presidio_entities, 'LOCATION')
  , version = version + 1
WHERE 'LOCATION' = ANY(presidio_entities)
  AND deleted IS FALSE;
