ALTER TABLE `risk_findings` DROP INDEX `idx_risk_findings_chat_id`;
ALTER TABLE `risk_findings` DROP COLUMN `false_positive_at`;
ALTER TABLE `risk_findings` DROP COLUMN `category`;
ALTER TABLE `risk_findings` DROP COLUMN `external_user_id`;
ALTER TABLE `risk_findings` DROP COLUMN `user_id`;
ALTER TABLE `risk_findings` DROP COLUMN `chat_id`;
