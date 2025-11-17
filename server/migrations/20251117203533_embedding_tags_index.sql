-- atlas:txmode none

-- Create index "toolset_embeddings_tags_idx" to table: "toolset_embeddings"
CREATE INDEX CONCURRENTLY "toolset_embeddings_tags_idx" ON "toolset_embeddings" USING gin ("tags");
