-- Modify "sigint_sensors" table
ALTER TABLE "sigint_sensors" ADD COLUMN "input_strategy" text NULL, ADD COLUMN "window_size" integer NULL;
-- Set comment to column: "input_strategy" on table: "sigint_sensors"
COMMENT ON COLUMN "sigint_sensors"."input_strategy" IS 'Absent means single_message; sliding_window supplies preceding conversation context.
@access: confidential';
-- Set comment to column: "window_size" on table: "sigint_sensors"
COMMENT ON COLUMN "sigint_sensors"."window_size" IS 'Total messages including the target; absent means five for sliding windows.
@access: confidential';
