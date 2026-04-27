-- Remove non-PII entities that were incorrectly backfilled into the PII
-- category by 20260424180559_risk-policies-default-pii.sql. These cause
-- excessive false positives in a coding context:
--   URL         - every URL in code/docs/configs
--   IP_ADDRESS  - common in configs, logs, networking code
--   MAC_ADDRESS - common in networking code
--   DATE_TIME   - ubiquitous in any codebase
--   NRP         - nationality/religious/political group, not PII
UPDATE risk_policies
SET presidio_entities = array_remove(
      array_remove(
        array_remove(
          array_remove(
            array_remove(presidio_entities, 'URL'),
          'IP_ADDRESS'),
        'MAC_ADDRESS'),
      'DATE_TIME'),
    'NRP')
  , version = version + 1
WHERE presidio_entities && '{URL,IP_ADDRESS,MAC_ADDRESS,DATE_TIME,NRP}'
  AND deleted IS FALSE;
