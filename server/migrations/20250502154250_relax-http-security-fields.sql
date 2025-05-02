-- Modify "http_security" table
ALTER TABLE "http_security" ALTER COLUMN "name" DROP NOT NULL, ALTER COLUMN "in_placement" DROP NOT NULL;
