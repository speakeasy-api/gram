-- Modify "workload_identity_admissions" table
ALTER TABLE "workload_identity_admissions" ADD CONSTRAINT "workload_identity_admissions_tags_check" CHECK (array_length(tags, 1) <= 40), ADD COLUMN "tags" text[] NOT NULL DEFAULT ARRAY[]::text[];
-- Modify "workload_issuers" table
ALTER TABLE "workload_issuers" ADD CONSTRAINT "workload_issuers_description_check" CHECK (char_length(description) <= 500), ADD COLUMN "description" text NULL;
