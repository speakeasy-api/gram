-- Create "platform_switches" table
CREATE TABLE "platform_switches" (
  "key" text NOT NULL,
  "engaged" boolean NOT NULL DEFAULT false,
  "updated_by" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("key"),
  CONSTRAINT "platform_switches_key_check" CHECK (key <> ''::text)
);
-- Set comment to column: "key" on table: "platform_switches"
COMMENT ON COLUMN "platform_switches"."key" IS '@access: confidential';
-- Set comment to column: "engaged" on table: "platform_switches"
COMMENT ON COLUMN "platform_switches"."engaged" IS '@access: confidential';
-- Set comment to column: "updated_by" on table: "platform_switches"
COMMENT ON COLUMN "platform_switches"."updated_by" IS 'Email of the staff member who last changed the switch.
@access: confidential-pii';
-- Set comment to column: "created_at" on table: "platform_switches"
COMMENT ON COLUMN "platform_switches"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "platform_switches"
COMMENT ON COLUMN "platform_switches"."updated_at" IS '@access: confidential';
