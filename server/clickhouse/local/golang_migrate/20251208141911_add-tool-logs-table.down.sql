-- reverse: create "tool_logs" table
DROP TABLE `tool_logs`;
ALTER TABLE `http_requests_raw` MODIFY COLUMN `id` REMOVE COMMENT;
