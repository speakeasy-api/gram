-- atlas:txmode none

-- Modify "corpus_feedback" table
ALTER TABLE "corpus_feedback" DROP CONSTRAINT "corpus_feedback_project_id_file_path_user_id_key";
-- Create index "corpus_feedback_project_id_file_path_idx" to table: "corpus_feedback"
CREATE INDEX CONCURRENTLY "corpus_feedback_project_id_file_path_idx" ON "corpus_feedback" ("project_id", "file_path");
-- Create index "corpus_feedback_project_id_file_path_user_id_created_at_idx" to table: "corpus_feedback"
CREATE INDEX CONCURRENTLY "corpus_feedback_project_id_file_path_user_id_created_at_idx" ON "corpus_feedback" ("project_id", "file_path", "user_id", "created_at" DESC);
