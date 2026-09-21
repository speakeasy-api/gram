ALTER TABLE `gram`.`risk_findings`
  COMMENT COLUMN `event_kind` 'Kind of this copy of the finding: finding (scanner output, dead-letter sentinels included), suppression or unsuppression (appended state-change copies from manual dismiss/undo and the retroactive exclusion reconcile). Empty on rows written before this column existed - such rows rank as finding copies.',
  ADD COLUMN `execution_id` String DEFAULT '' COMMENT 'Correlation ID of one mediated execution. Empty for legacy and chat-only findings.' CODEC(ZSTD(1)) AFTER `event_kind`,
  ADD COLUMN `mcp_server_id` String DEFAULT '' COMMENT 'Canonical concrete MCP server ID. Empty when unavailable.' CODEC(ZSTD(1)) AFTER `execution_id`,
  ADD COLUMN `meta_mcp_server_id` String DEFAULT '' COMMENT 'Outer meta MCP gateway ID, separate from the concrete member server.' CODEC(ZSTD(1)) AFTER `mcp_server_id`,
  ADD COLUMN `toolset_id` String DEFAULT '' COMMENT 'Persisted toolset ID, empty for runtime-only or unavailable toolsets.' CODEC(ZSTD(1)) AFTER `meta_mcp_server_id`,
  ADD COLUMN `tool_name` String DEFAULT '' COMMENT 'Resolved concrete tool name, empty when unavailable.' CODEC(ZSTD(1)) AFTER `toolset_id`,
  ADD COLUMN `phase` LowCardinality(String) DEFAULT '' COMMENT 'Inspection phase: request or response. Empty for legacy findings.' AFTER `tool_name`,
  ADD COLUMN `mediation_surface` LowCardinality(String) DEFAULT '' COMMENT 'Concrete mediation seam producing the finding.' AFTER `phase`,
  ADD COLUMN `mcp_method` LowCardinality(String) DEFAULT '' COMMENT 'MCP method or equivalent mediated operation, such as tools/call.' AFTER `mediation_surface`,
  ADD COLUMN `principal_kind` LowCardinality(String) DEFAULT '' COMMENT 'Credential class established exclusively by mcpidentity. Empty when unstamped.' AFTER `mcp_method`,
  ADD COLUMN `identity_stamped` Bool DEFAULT false COMMENT 'Whether validated principal provenance was present, including validated anonymous sessions.' AFTER `principal_kind`,
  ADD COLUMN `enforcement_outcome` Enum8('' = 0, 'logged' = 1, 'denied' = 2, 'withheld' = 3, 'warned_pending' = 4, 'warned_acknowledged' = 5, 'warned_abandoned' = 6, 'quarantined' = 7) DEFAULT '' COMMENT 'Enforcement action taken, independent of detection. Empty when unspecified or legacy.',
  ADD INDEX `idx_risk_findings_mcp_server_id` ((mcp_server_id)) TYPE bloom_filter(0.01);
-- materialize index "idx_risk_findings_mcp_server_id" for existing data
ALTER TABLE `gram`.`risk_findings` MATERIALIZE INDEX `idx_risk_findings_mcp_server_id`;
