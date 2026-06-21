-- Modify "assistant_memories" table
ALTER TABLE "assistant_memories" ADD COLUMN "source_kind" text NULL, ADD COLUMN "source_user_id" text NULL, ADD COLUMN "source_correlation_id" text NULL, ADD COLUMN "source_timestamp" timestamptz NULL;
