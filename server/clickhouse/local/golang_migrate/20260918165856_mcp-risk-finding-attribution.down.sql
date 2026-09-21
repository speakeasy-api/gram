ALTER TABLE `gram`.`risk_findings`
  DROP INDEX `idx_risk_findings_mcp_server_id`,
  DROP COLUMN `enforcement_outcome`,
  DROP COLUMN `identity_stamped`,
  DROP COLUMN `principal_kind`,
  DROP COLUMN `mcp_method`,
  DROP COLUMN `mediation_surface`,
  DROP COLUMN `phase`,
  DROP COLUMN `tool_name`,
  DROP COLUMN `toolset_id`,
  DROP COLUMN `meta_mcp_server_id`,
  DROP COLUMN `mcp_server_id`,
  DROP COLUMN `execution_id`,
  COMMENT COLUMN `event_kind` 'Kind of this copy of the finding: finding (scanner output, dead-letter sentinels included), suppression or unsuppression (appended state-change copies from manual dismiss/undo and the retroactive exclusion reconcile). Empty on rows written before the column existed - such rows rank as finding copies.';
