-- Modify "packages" table
ALTER TABLE "packages" ADD CONSTRAINT "packages_url_check" CHECK ((url <> ''::text) AND (char_length(url) <= 200)), ADD COLUMN "url" text NULL;
