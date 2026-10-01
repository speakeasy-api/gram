-- Modify "plugins" table
ALTER TABLE "plugins" ADD COLUMN "auto_created" boolean NOT NULL DEFAULT false;
