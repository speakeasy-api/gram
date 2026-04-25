-- Enable PII scanning on all existing risk policies that only have gitleaks.
UPDATE risk_policies
SET sources = sources || '{presidio}'
  , presidio_entities = '{PERSON,EMAIL_ADDRESS,PHONE_NUMBER,LOCATION,IP_ADDRESS,URL,MAC_ADDRESS,DATE_TIME,NRP,CREDIT_CARD,CRYPTO,IBAN_CODE,US_BANK_NUMBER,US_SSN,US_ITIN,US_PASSPORT,US_DRIVER_LICENSE,MEDICAL_LICENSE,UK_NHS}'
  , version = version + 1
WHERE NOT sources @> '{presidio}'
  AND deleted IS FALSE;
