-- Modify "users" table
ALTER TABLE "users" ADD COLUMN "deleted_at" timestamptz NULL;
