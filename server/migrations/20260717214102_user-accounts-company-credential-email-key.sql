-- atlas:txmode none

-- Modify "user_accounts" table
ALTER TABLE "user_accounts" ALTER COLUMN "external_account_uuid" DROP NOT NULL;
-- Create index "user_accounts_org_provider_email_key" to table: "user_accounts"
CREATE UNIQUE INDEX CONCURRENTLY "user_accounts_org_provider_email_key" ON "user_accounts" ("organization_id", "provider", "email") WHERE ((external_account_uuid IS NULL) AND (deleted_at IS NULL));
