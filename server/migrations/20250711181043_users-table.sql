-- Create "users" table
CREATE TABLE "users" ("id" text NOT NULL, "email" text NOT NULL, "display_name" text NOT NULL, "photo_url" text NULL, "admin" boolean NOT NULL DEFAULT false, "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(), "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY ("id"));
-- Create index "users_email_key" to table: "users"
CREATE UNIQUE INDEX "users_email_key" ON "users" ("email");
