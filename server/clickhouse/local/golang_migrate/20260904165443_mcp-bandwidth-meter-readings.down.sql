ALTER TABLE `billing_meter_readings` DROP COLUMN `measurement_method`;
ALTER TABLE `billing_meter_readings` COMMENT COLUMN `unit` 'Measurement unit, currently stokens.';
ALTER TABLE `billing_meter_readings` ADD CONSTRAINT `measurement_valid` CHECK ((unit = 'stokens') AND (tokenizer_codec = 'tiktoken_o200k_base'));
ALTER TABLE `billing_meter_readings` MODIFY COMMENT 'Raw s-token usage ledger with producer-time stable-id convergence and billing reads requiring FINAL or equivalent id deduplication';
