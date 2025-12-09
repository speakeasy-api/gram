ALTER TABLE `tool_logs` ADD COLUMN `id` UUID DEFAULT generateUUIDv7() COMMENT 'Unique identifier for the log entry.';
