-- atlas:txmode none

-- Create index "risk_meter_reading_acceptances_project_id_idx" to table: "risk_meter_reading_acceptances"
CREATE INDEX CONCURRENTLY "risk_meter_reading_acceptances_project_id_idx" ON "risk_meter_reading_acceptances" ("project_id");
