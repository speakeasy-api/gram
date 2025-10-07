-- Modify "tool_variations_groups" table
ALTER TABLE "tool_variations_groups" ADD COLUMN "version" bigint NOT NULL DEFAULT 1;
