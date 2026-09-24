ALTER TABLE `billing_meter_readings` MODIFY COMMENT 'Raw usage ledger with producer-time stable-id convergence and billing reads requiring FINAL or equivalent id deduplication';
ALTER TABLE `billing_meter_readings` DROP CONSTRAINT `measurement_valid`;
ALTER TABLE `billing_meter_readings` COMMENT COLUMN `unit` 'Measurement unit.';
ALTER TABLE `billing_meter_readings` ADD COLUMN `measurement_method` LowCardinality(String) DEFAULT if(unit = 'stokens', 'tiktoken_o200k_base', '') COMMENT 'Rating-critical measurement implementation.';
