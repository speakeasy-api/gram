-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" DROP COLUMN "account_type", ADD COLUMN "gram_account_type" text NOT NULL DEFAULT 'free';
