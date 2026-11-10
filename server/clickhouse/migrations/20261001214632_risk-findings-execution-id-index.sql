ALTER TABLE `gram`.`risk_findings` ADD INDEX `idx_risk_findings_execution_id` ((execution_id)) TYPE bloom_filter(0.01);
-- Materialize index "idx_risk_findings_execution_id" for existing data
ALTER TABLE `gram`.`risk_findings` MATERIALIZE INDEX `idx_risk_findings_execution_id`;
