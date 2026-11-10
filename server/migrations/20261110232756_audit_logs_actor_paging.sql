-- atlas:txmode none

-- Create index "audit_logs_organization_id_actor_id_seq_idx" to table: "audit_logs"
CREATE INDEX CONCURRENTLY "audit_logs_organization_id_actor_id_seq_idx" ON "audit_logs" ("organization_id", "actor_id", "seq" DESC);
