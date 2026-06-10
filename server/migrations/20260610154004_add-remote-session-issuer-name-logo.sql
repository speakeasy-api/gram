-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD CONSTRAINT "remote_session_issuers_name_check" CHECK ((name IS NULL) OR (name <> ''::text)), ADD COLUMN "name" text NULL, ADD COLUMN "logo_asset_id" uuid NULL, ADD CONSTRAINT "remote_session_issuers_logo_asset_id_fkey" FOREIGN KEY ("logo_asset_id") REFERENCES "assets" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
