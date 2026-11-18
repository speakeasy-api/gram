ALTER TABLE `gram`.`sigint_readings_v2`
  ADD COLUMN `conversation_generation` Int32 DEFAULT 0 COMMENT 'Captured transcript generation.
@access: confidential' AFTER `conversation_role`,
  ADD COLUMN `conversation_sequence` Int64 DEFAULT 0 COMMENT 'Captured source message sequence.
@access: confidential' AFTER `conversation_generation`,
  ADD COLUMN `conversation_capture_kind` LowCardinality(String) DEFAULT '' COMMENT 'Original, reobserved, synthetic or unknown source lineage. Empty means unavailable.
@access: confidential' AFTER `conversation_sequence`,
  ADD COLUMN `conversation_original_message_id` Nullable(UUID) COMMENT 'Original source message for an exact transcript copy.
@access: confidential' AFTER `conversation_capture_kind`;
