-- reverse: create "session_transcript_mv" view
DROP VIEW `gram`.`session_transcript_mv`;
-- reverse: create "session_transcript" table
DROP TABLE `gram`.`session_transcript`;
ALTER TABLE `gram`.`agent_events` DROP COLUMN `channel`;
