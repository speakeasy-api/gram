-- Remove non-PII entities that were incorrectly backfilled into the PII
-- category by 20260424180559_risk-policies-default-pii.sql. These are not
-- personally identifiable information:
--   URL       - not PII, every URL in code/docs/configs would trigger
--   DATE_TIME - not PII, ubiquitous in any codebase
--   NRP       - nationality/religious/political group, not PII
UPDATE risk_policies
SET presidio_entities = array_remove(
      array_remove(
        array_remove(presidio_entities, 'URL'),
      'DATE_TIME'),
    'NRP')
  , version = version + 1
WHERE presidio_entities && '{URL,DATE_TIME,NRP}'
  AND deleted IS FALSE;
