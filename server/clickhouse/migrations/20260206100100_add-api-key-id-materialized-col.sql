ALTER TABLE `telemetry_logs` ADD COLUMN `api_key_id` String MATERIALIZED toString(attributes.gram.api_key.id) COMMENT 'API key ID (materialized from attributes.gram.api_key.id).';
