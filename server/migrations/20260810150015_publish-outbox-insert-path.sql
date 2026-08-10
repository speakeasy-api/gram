-- atlas:txmode none

-- Drop index "publish_outbox_public_id_key" from table: "publish_outbox"
DROP INDEX CONCURRENTLY "publish_outbox_public_id_key";
-- Modify "publish_outbox" table
ALTER TABLE "publish_outbox" DROP CONSTRAINT "publish_outbox_organization_id_fkey";
-- Set comment to column: "public_id" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."public_id" IS 'Stable id a producer can put inside its own message body. Deliberately unindexed: nothing looks a row up by it, so an index here would buy nothing and cost a uniqueness check on the caller''s transaction. Collisions are prevented by minting uuidv7, not by the database.';
-- Set comment to column: "organization_id" on table: "publish_outbox"
COMMENT ON COLUMN "publish_outbox"."organization_id" IS 'Owning organization, carried through to the published message. Deliberately not a foreign key: the check would take a KEY SHARE lock on the organization row for every enqueue, and a stream of those against one busy org generates multixacts on a row that other writers update. Rows live seconds and the relay never joins to the organization, so an org deleted mid-flight leaves rows that publish and then delete themselves.';
