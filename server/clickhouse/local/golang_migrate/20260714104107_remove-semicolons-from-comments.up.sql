-- Hand-written to mirror migrations/20260714104103_remove-semicolons-from-comments.sql.
-- Atlas's generated golang-migrate diff surfaced unrelated drift (missing
-- chat_token_summaries/tum_breakdown_summaries migrations) instead of these
-- comment changes, because the golang-migrate copies of the earlier
-- migrations were edited in place to strip the semicolons that break
-- golang-migrate's naive statement splitter. This keeps existing databases
-- in sync with those edits.
ALTER TABLE `telemetry_logs` COMMENT COLUMN `provider` 'AI provider for the session account (e.g. anthropic, openai). Set by ingest (materialized from attributes.gram.provider).';
ALTER TABLE `telemetry_logs` COMMENT COLUMN `external_org_id` 'Provider organization id for the account the user was logged into on-device (e.g. Claude organization.id). Distinct from the Gram org. Personal-account tracking discriminator. Normalized by ingest (materialized from attributes.gram.external_org_id).';
ALTER TABLE `telemetry_logs` COMMENT COLUMN `account_type` 'team (company/enterprise account) or personal (individual account). Set by ingest. Empty until classified (materialized from attributes.gram.account_type).';
ALTER TABLE `telemetry_logs` COMMENT COLUMN `billing_mode` 'How the account is billed: metered (pay-per-token, cost is real spend) | flat_rate (subscription seat, cost is an estimate) | unknown | empty. Resolved by ingest from admin-declared config (materialized from attributes.gram.billing_mode).';
