-- Modify "packages" table
ALTER TABLE "packages" ADD COLUMN "image_asset_id" uuid NULL, ADD CONSTRAINT "packages_image_asset_id_fkey" FOREIGN KEY ("image_asset_id") REFERENCES "assets" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
