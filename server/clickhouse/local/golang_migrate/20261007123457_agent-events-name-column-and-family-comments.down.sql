ALTER TABLE `gram`.`agent_events`
  DROP COLUMN `name`,
  COMMENT COLUMN `output_content` 'Normalized output message JSON. Empty when the record carries none.',
  COMMENT COLUMN `input_content` 'Normalized input message JSON. Empty when the record carries none.',
  COMMENT COLUMN `tool_name` 'Tool the record concerns, when the record says so. Empty otherwise.',
  COMMENT COLUMN `mcp_tool_name` 'MCP tool involved, when the record says so. Empty otherwise.',
  COMMENT COLUMN `mcp_server_name` 'MCP server involved, when the record says so. Empty otherwise.',
  COMMENT COLUMN `agent_name` 'Sub-agent name, when the record says so. Empty otherwise.',
  COMMENT COLUMN `skill_name` 'Skill invoked, when the record says so. Empty otherwise.';
