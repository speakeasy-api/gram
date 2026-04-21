-- atlas:txmode none

-- Create index "deployment_statuses_deployment_id_seq_idx" to table: "deployment_statuses"
CREATE INDEX CONCURRENTLY "deployment_statuses_deployment_id_seq_idx" ON "deployment_statuses" ("deployment_id", "seq" DESC);
