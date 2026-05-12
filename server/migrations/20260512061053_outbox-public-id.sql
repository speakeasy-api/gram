-- atlas:txmode none

-- Modify "outbox" table
ALTER TABLE "outbox" ADD COLUMN "public_id" uuid NOT NULL DEFAULT generate_uuidv7();
-- Create index "outbox_public_id_key" to table: "outbox"
CREATE UNIQUE INDEX CONCURRENTLY "outbox_public_id_key" ON "outbox" ("public_id");
