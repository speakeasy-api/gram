-- Modify "chats" table
ALTER TABLE "chats" ADD COLUMN "agent_id" uuid NULL;
-- Set comment to column: "agent_id" on table: "chats"
COMMENT ON COLUMN "chats"."agent_id" IS '@access: confidential';
