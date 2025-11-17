-- Modify "toolset_embeddings" table
ALTER TABLE "toolset_embeddings" ADD COLUMN "tags" text[] NULL DEFAULT ARRAY[]::text[];
