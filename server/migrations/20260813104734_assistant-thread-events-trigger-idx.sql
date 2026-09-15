-- atlas:txmode none

-- Create index "assistant_thread_events_project_id_trigger_created_at_idx" to table: "assistant_thread_events"
CREATE INDEX CONCURRENTLY "assistant_thread_events_project_id_trigger_created_at_idx" ON "assistant_thread_events" ("project_id", "trigger_instance_id", "created_at" DESC) WHERE ((deleted IS FALSE) AND (trigger_instance_id IS NOT NULL));
