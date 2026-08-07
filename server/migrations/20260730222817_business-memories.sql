-- Create "business_memories" table
CREATE TABLE "business_memories" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NULL,
  "organization_id" text NOT NULL,
  "body" text NOT NULL,
  "memory_type" text NOT NULL,
  "structural_scope" text NOT NULL,
  "content_scope" jsonb NOT NULL DEFAULT '[]',
  "embedding" halfvec(1024) NOT NULL,
  "embedding_model" text NOT NULL,
  "extraction_model" text NOT NULL,
  "source_evaluation_id" uuid NULL,
  "source_candidate_index" integer NOT NULL,
  "source_chat_id" uuid NULL,
  "source_turn" integer NULL,
  "source_author_id" text NULL,
  "extracted_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "lifecycle_state" text NOT NULL DEFAULT 'active',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "business_memories_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "business_memories_source_chat_id_fkey" FOREIGN KEY ("source_chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "business_memories_source_evaluation_id_fkey" FOREIGN KEY ("source_evaluation_id") REFERENCES "chat_analysis_evaluations" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "business_memories_body_size_check" CHECK (octet_length(body) <= 8192)
);
-- Create index "business_memories_content_scope_gin_idx" to table: "business_memories"
CREATE INDEX "business_memories_content_scope_gin_idx" ON "business_memories" USING gin ("content_scope") WHERE (deleted IS FALSE);
-- Create index "business_memories_embedding_hnsw_idx" to table: "business_memories"
CREATE INDEX "business_memories_embedding_hnsw_idx" ON "business_memories" USING hnsw ("embedding" halfvec_cosine_ops) WHERE ((deleted IS FALSE) AND (lifecycle_state = 'active'::text));
-- Create index "business_memories_project_created_at_idx" to table: "business_memories"
CREATE INDEX "business_memories_project_created_at_idx" ON "business_memories" ("project_id", "created_at" DESC, "id" DESC) WHERE (deleted IS FALSE);
-- Create index "business_memories_project_id_idx" to table: "business_memories"
CREATE INDEX "business_memories_project_id_idx" ON "business_memories" ("project_id");
-- Create index "business_memories_source_candidate_key" to table: "business_memories"
CREATE UNIQUE INDEX "business_memories_source_candidate_key" ON "business_memories" ("source_evaluation_id", "source_candidate_index");
