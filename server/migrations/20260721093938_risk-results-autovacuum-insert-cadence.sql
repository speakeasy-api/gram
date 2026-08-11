-- Set "autovacuum_vacuum_insert_scale_factor" storage parameter on table: "risk_results"
ALTER TABLE "risk_results" SET (autovacuum_vacuum_insert_scale_factor = 0);
-- Set "autovacuum_vacuum_insert_threshold" storage parameter on table: "risk_results"
ALTER TABLE "risk_results" SET (autovacuum_vacuum_insert_threshold = 250000);
-- Set "autovacuum_vacuum_cost_limit" storage parameter on table: "risk_results"
ALTER TABLE "risk_results" SET (autovacuum_vacuum_cost_limit = 2000);
