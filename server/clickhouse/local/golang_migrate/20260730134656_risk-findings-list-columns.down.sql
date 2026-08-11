ALTER TABLE `risk_findings` DROP INDEX `idx_risk_findings_assistant_id`;
ALTER TABLE `risk_findings` DROP COLUMN `assistant_id`;
ALTER TABLE `risk_findings` DROP COLUMN `message_created_at`;
