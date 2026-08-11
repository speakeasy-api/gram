ALTER TABLE `risk_findings` ADD COLUMN `content_part_id` String DEFAULT '' COMMENT 'Chat content part the finding was detected in.' CODEC(ZSTD(1));
ALTER TABLE `risk_findings` ADD INDEX `idx_risk_findings_content_part_id` ((content_part_id)) TYPE bloom_filter(0.01) GRANULARITY 1;
