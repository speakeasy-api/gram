ALTER TABLE `risk_findings` DROP COLUMN `tool_call_id`;
ALTER TABLE `risk_findings` DROP COLUMN `path`;
ALTER TABLE `risk_findings` DROP COLUMN `field`;
ALTER TABLE `risk_findings` DROP COLUMN `surface`;
ALTER TABLE `risk_findings` COMMENT COLUMN `match_redacted` 'Precomputed display string in the form redacted len=N sha=XXXXXXXX. Every source is redacted here including shadow_mcp and account_identity so no plaintext or PII is ever stored in ClickHouse. The verbatim value stays in Postgres for the audited unmask path.';
