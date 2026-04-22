-- Update column comment to reflect renamed scope (build:read → project:read).
COMMENT ON COLUMN "principal_grants"."scope" IS 'The scope being granted, e.g. "project:read". Validated in application code, not via FK.';
