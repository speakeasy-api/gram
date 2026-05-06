-- Create "assistant_memories" table
CREATE TABLE "assistant_memories" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "assistant_id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "content" text NOT NULL,
  "embedding" halfvec(4000) NOT NULL,
  "supersedes_id" uuid NULL,
  "superseded_at" timestamptz NULL,
  "valid_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "tags" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "origin_thread_id" uuid NULL,
  "origin_chat_id" uuid NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "last_access" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "assistant_memories_assistant_id_fkey" FOREIGN KEY ("assistant_id") REFERENCES "assistants" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_memories_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_memories_supersedes_id_fkey" FOREIGN KEY ("supersedes_id") REFERENCES "assistant_memories" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_memories_content_size_check" CHECK (octet_length(content) <= 8192)
);
-- Create index "assistant_memories_assistant_active" to table: "assistant_memories"
CREATE INDEX "assistant_memories_assistant_active" ON "assistant_memories" ("assistant_id", "created_at" DESC) WHERE (deleted_at IS NULL);
-- Create index "assistant_memories_embedding_hnsw" to table: "assistant_memories"
CREATE INDEX "assistant_memories_embedding_hnsw" ON "assistant_memories" USING hnsw ("embedding" halfvec_cosine_ops) WHERE ((deleted_at IS NULL) AND (superseded_at IS NULL));
-- Create index "assistant_memories_tags_gin" to table: "assistant_memories"
CREATE INDEX "assistant_memories_tags_gin" ON "assistant_memories" USING gin ("tags") WHERE (deleted_at IS NULL);
