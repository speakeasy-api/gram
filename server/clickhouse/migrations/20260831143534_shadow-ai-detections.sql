-- Create "ai_detections" table
CREATE TABLE `ai_detections` (
  `organization_id` String COMMENT 'Organization the reporting device agent is enrolled in.',
  `target_id` LowCardinality(String) COMMENT 'Id of the detected AI tool as reported by the agent. Usually a server/internal/agent/aitargets catalog id, but stored as-is: agent binaries can ship newer target lists than the catalog knows.',
  `device_serial` String COMMENT 'Normalized hardware serial of the reporting device. Empty when the agent cannot read one.',
  `user_email` String COMMENT 'Normalized email of the enrolled user the scan is attributed to.',
  `signal` LowCardinality(String) COMMENT 'Scan observation value validated by application code. Currently installed or running.',
  `category` LowCardinality(String) COMMENT 'Detection category validated by application code. Currently harness or local_model. Known targets use the server catalog, while unknown ids keep a supported reported category.',
  `version` String COMMENT 'Installed version when the scan could extract one statically. Empty otherwise.',
  `first_seen` DateTime64(9, 'UTC') COMMENT 'When this signal was first reported for this org, target, device, user. Preserved across upserts via read-merge-write.',
  `last_seen` DateTime64(9, 'UTC') COMMENT 'When this signal was most recently reported.',
  `updated_at` DateTime64(9, 'UTC') COMMENT 'Replacing-merge version column. The newest row per key wins.'
) ENGINE = ReplacingMergeTree(updated_at)
PRIMARY KEY (`organization_id`, `target_id`, `device_serial`, `user_email`, `signal`) ORDER BY (`organization_id`, `target_id`, `device_serial`, `user_email`, `signal`) SETTINGS index_granularity = 8192 COMMENT 'Org-scoped AI tool detections reported by device-agent AI scans - one row per organization, target, device, user, signal';
-- Create "ai_scan_receipts" table
CREATE TABLE `ai_scan_receipts` (
  `organization_id` String COMMENT 'Organization the reporting device agent is enrolled in.',
  `device_serial` String COMMENT 'Normalized hardware serial of the reporting device. Empty when the agent cannot read one.',
  `user_email` String COMMENT 'Normalized email of the enrolled user the scan is attributed to.',
  `scan_started_at` DateTime64(9, 'UTC') COMMENT 'When the agent started the scan, as reported.',
  `scan_completed_at` DateTime64(9, 'UTC') COMMENT 'When the agent completed the scan, as reported.',
  `target_list_version` Int32 COMMENT 'Version of the target list compiled into the agent binary that ran the scan, echoed as reported.',
  `match_count` UInt32 COMMENT 'Number of matches reported by the agent. Zero-match scans still post a receipt so coverage is provable.',
  `received_at` DateTime64(9, 'UTC') COMMENT 'When the server received the report.'
) ENGINE = MergeTree
PRIMARY KEY (`organization_id`, `device_serial`, `received_at`) ORDER BY (`organization_id`, `device_serial`, `received_at`) PARTITION BY (toYYYYMM(received_at)) TTL toDateTime(received_at) + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'Append-only receipts for device-agent AI scans, proving a device scanned even when nothing matched';
