-- Modify "organization_user_relationships" table
ALTER TABLE "organization_user_relationships" DROP CONSTRAINT "organization_user_relationships_user_id_fkey", ALTER COLUMN "user_id" DROP NOT NULL;
