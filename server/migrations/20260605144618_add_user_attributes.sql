-- Create "user_attributes" table
CREATE TABLE "user_attributes" (
  "id" text NOT NULL,
  "user_id" text NOT NULL,
  "attributes" jsonb NOT NULL,
  "content_hash" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "user_attributes_user_id_fkey" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "user_attributes_user_history" to table: "user_attributes"
CREATE INDEX "user_attributes_user_history" ON "user_attributes" ("user_id", "created_at" DESC);
