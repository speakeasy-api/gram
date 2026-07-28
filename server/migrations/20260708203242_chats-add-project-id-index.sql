-- atlas:txmode none

-- Create index "chats_project_id_idx" to table: "chats"
CREATE INDEX CONCURRENTLY "chats_project_id_idx" ON "chats" ("project_id");
