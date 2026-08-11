ALTER TABLE `risk_findings` ADD COLUMN `chat_id` String DEFAULT '' COMMENT 'Denormalized chats.id of the chat the finding was detected in. Empty when unresolved.' CODEC(ZSTD(1));
ALTER TABLE `risk_findings` ADD COLUMN `user_id` String DEFAULT '' COMMENT 'Resolved internal user id: chat_messages.user_id with fallback to chats.user_id. Empty when unresolved.' CODEC(ZSTD(1));
ALTER TABLE `risk_findings` ADD COLUMN `external_user_id` String DEFAULT '' COMMENT 'Resolved external user id: chat_messages.external_user_id with fallback to chats.external_user_id. Empty when unresolved.' CODEC(ZSTD(1));
ALTER TABLE `risk_findings` ADD COLUMN `category` LowCardinality(String) DEFAULT '' COMMENT 'Risk category derived from rule_id and source at ingest, e.g. pii or secrets. Empty when the rule maps to no category.';
ALTER TABLE `risk_findings` ADD COLUMN `false_positive_at` Nullable(DateTime64(9)) COMMENT 'Time the finding was marked a false positive, mirrored from Postgres after the fact. Null when the finding is not marked.' CODEC(DoubleDelta, ZSTD(1));
ALTER TABLE `risk_findings` ADD INDEX `idx_risk_findings_chat_id` ((chat_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
