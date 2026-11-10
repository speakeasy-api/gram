ALTER TABLE `gram`.`sigint_readings_v2`
  DROP COLUMN `conversation_original_message_id`,
  DROP COLUMN `conversation_capture_kind`,
  DROP COLUMN `conversation_sequence`,
  DROP COLUMN `conversation_generation`;
