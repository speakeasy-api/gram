-- Modify "assistants" table
ALTER TABLE "assistants" ADD COLUMN "reasoning_effort" text NULL DEFAULT 'medium';
-- Set comment to column: "reasoning_effort" on table: "assistants"
COMMENT ON COLUMN "assistants"."reasoning_effort" IS '@access: confidential';
