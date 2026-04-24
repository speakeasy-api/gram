-- Enable PII scanning on all existing risk policies that only have gitleaks.
UPDATE risk_policies
SET sources = sources || '{presidio}'
  , presidio_entities = '{PERSON,EMAIL_ADDRESS,PHONE_NUMBER,LOCATION,IP_ADDRESS,URL,MAC_ADDRESS,DATE_TIME,NRP}'
  , version = version + 1
WHERE NOT sources @> '{presidio}'
  AND deleted IS FALSE;
