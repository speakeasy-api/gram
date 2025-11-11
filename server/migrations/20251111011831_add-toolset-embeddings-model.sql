-- Create "toolset_embeddings" table
CREATE TABLE "toolset_embeddings" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "toolset_id" uuid NOT NULL,
  "entry_key" text NOT NULL,
  "embedding_model" text NOT NULL,
  "embedding_1536" vector(1536) NOT NULL,
  "payload" jsonb NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "toolset_embeddings_project_id" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "toolset_embeddings_embedding_model_check" CHECK ((embedding_model <> ''::text) AND (char_length(embedding_model) <= 100)),
  CONSTRAINT "toolset_embeddings_entry_key_check" CHECK ((entry_key <> ''::text) AND (char_length(entry_key) <= 255))
);
-- Create index "toolset_embeddings_embedding_idx" to table: "toolset_embeddings"
CREATE INDEX "toolset_embeddings_embedding_idx" ON "toolset_embeddings" USING hnsw ("embedding_1536" vector_cosine_ops) WHERE (deleted IS FALSE);
-- Create index "toolset_embeddings_toolset_entry_key" to table: "toolset_embeddings"
CREATE UNIQUE INDEX "toolset_embeddings_toolset_entry_key" ON "toolset_embeddings" ("toolset_id", "entry_key") WHERE (deleted IS FALSE);
