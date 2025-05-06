-- Modify "packages" table
ALTER TABLE "packages" ADD CONSTRAINT "packages_description_raw_check" CHECK ((description_raw <> ''::text) AND (char_length(description_raw) <= 10000)), ADD COLUMN "description_raw" text NULL, ADD COLUMN "description_html" text NULL;
