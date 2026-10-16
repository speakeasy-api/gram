-- Modify "sigint_sensors" table
ALTER TABLE "sigint_sensors" ADD COLUMN "min_confidence" double precision NULL;
-- Set comment to column: "min_confidence" on table: "sigint_sensors"
COMMENT ON COLUMN "sigint_sensors"."min_confidence" IS 'Classifier confidence (0 to 1) below which answers count as uncertain when readings are queried; absent means every answer counts.
@access: confidential';
