-- atlas:txmode none

-- Modify "chats" table
ALTER TABLE "chats" ADD COLUMN "inference_actor_key" bytea NULL;
-- Create index "chat_messages_inference_identity_idx" to table: "chat_messages"
CREATE INDEX CONCURRENTLY "chat_messages_inference_identity_idx" ON "chat_messages" ("project_id", "external_message_id") WHERE ((origin = 'anthropic-inference'::text) AND (external_message_id IS NOT NULL));
