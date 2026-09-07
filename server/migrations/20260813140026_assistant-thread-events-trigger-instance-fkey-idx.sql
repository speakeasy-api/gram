-- atlas:txmode none

-- Create index "assistant_thread_events_trigger_instance_id_idx" to table: "assistant_thread_events"
CREATE INDEX CONCURRENTLY "assistant_thread_events_trigger_instance_id_idx" ON "assistant_thread_events" ("trigger_instance_id");
