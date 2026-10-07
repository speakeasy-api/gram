ALTER TABLE `gram`.`agent_events` DROP COLUMN `name`;
ALTER TABLE `gram`.`agent_events` COMMENT COLUMN `output_content` 'Normalized output message JSON. Empty when the record carries none.';
ALTER TABLE `gram`.`agent_events` COMMENT COLUMN `input_content` 'Normalized input message JSON. Empty when the record carries none.';
ALTER TABLE `gram`.`agent_events` COMMENT COLUMN `tool_name` 'Tool the record concerns, when the record says so. Empty otherwise.';
ALTER TABLE `gram`.`agent_events` COMMENT COLUMN `mcp_tool_name` 'MCP tool involved, when the record says so. Empty otherwise.';
ALTER TABLE `gram`.`agent_events` COMMENT COLUMN `mcp_server_name` 'MCP server involved, when the record says so. Empty otherwise.';
ALTER TABLE `gram`.`agent_events` COMMENT COLUMN `agent_name` 'Sub-agent name, when the record says so. Empty otherwise.';
ALTER TABLE `gram`.`agent_events` COMMENT COLUMN `skill_name` 'Skill invoked, when the record says so. Empty otherwise.';
