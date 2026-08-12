-- Modify "publish_outbox" table
ALTER TABLE "publish_outbox" ADD COLUMN "lease_token" uuid NULL;
-- Set comment to column: "lease_token" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."lease_token" IS 'Identifies the claim currently holding the row, minted by the drainer. Settlement matches on it so a drain that outlived its lease cannot delete, dead-letter or release a row another drainer has since claimed. NULL means unclaimed. Unindexed, like locked_until, so claiming stays a HOT update.';
