-- atlas:txmode none

-- Drop index "assistant_runtimes_assistant_thread_id_active_key" from table: "assistant_runtimes"
DROP INDEX CONCURRENTLY "assistant_runtimes_assistant_thread_id_active_key";
-- Modify "assistant_runtimes" table
ALTER TABLE "assistant_runtimes" DROP CONSTRAINT "assistant_runtimes_assistant_thread_id_fkey", ADD COLUMN "runtime_version" smallint NOT NULL DEFAULT 1;
-- Create index "assistant_runtimes_assistant_thread_id_active_key" to table: "assistant_runtimes"
CREATE UNIQUE INDEX CONCURRENTLY "assistant_runtimes_assistant_thread_id_active_key" ON "assistant_runtimes" ("assistant_thread_id") WHERE ((deleted IS FALSE) AND (ended IS FALSE) AND (runtime_version = 1));
-- Create index "assistant_runtimes_v2_one_per_assistant" to table: "assistant_runtimes"
CREATE UNIQUE INDEX CONCURRENTLY "assistant_runtimes_v2_one_per_assistant" ON "assistant_runtimes" ("project_id", "assistant_id") WHERE ((runtime_version = 2) AND (deleted IS FALSE) AND (ended IS FALSE));
