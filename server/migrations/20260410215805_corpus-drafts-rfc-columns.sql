-- Modify "corpus_drafts" table
ALTER TABLE "corpus_drafts" ADD COLUMN "title" text NULL, ADD COLUMN "original_content" text NULL, ADD COLUMN "author_user_id" text NULL, ADD COLUMN "agent_name" text NULL;
