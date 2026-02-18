-- atlas:txmode none

-- Modify "chats" table
ALTER TABLE "chats" ADD COLUMN "source" text NULL, ADD COLUMN "connection_fingerprint" text NULL;
-- Create index "chats_fingerprint_idx" to table: "chats"
CREATE INDEX CONCURRENTLY "chats_fingerprint_idx" ON "chats" ("project_id", "connection_fingerprint") WHERE (deleted IS FALSE);
-- Create index "chats_source_idx" to table: "chats"
CREATE INDEX CONCURRENTLY "chats_source_idx" ON "chats" ("project_id", "source") WHERE (deleted IS FALSE);
