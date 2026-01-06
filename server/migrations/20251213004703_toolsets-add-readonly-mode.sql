-- Modify "toolsets" table
ALTER TABLE "toolsets" ADD COLUMN "readonly_mode" boolean NOT NULL DEFAULT false;
