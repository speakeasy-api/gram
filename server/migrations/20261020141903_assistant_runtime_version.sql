-- Modify "assistants" table
ALTER TABLE "assistants" ADD COLUMN "runtime_version" smallint NOT NULL DEFAULT 2;
-- Set comment to column: "runtime_version" on table: "assistants"
COMMENT ON COLUMN "assistants"."runtime_version" IS '@access: confidential';
