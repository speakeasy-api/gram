-- name: InsertPublishOutboxEntry :one
-- Enqueues a message for publication to a Pub/Sub topic. public_id is supplied
-- by the caller rather than defaulted so the same value can be embedded in the
-- message body before the row is written.
INSERT INTO publish_outbox (public_id, organization_id, topic, message, attributes)
VALUES (@public_id, @organization_id, @topic, @message, @attributes)
RETURNING id, public_id, created_at;

-- name: BulkInsertPublishOutboxEntries :copyfrom
INSERT INTO publish_outbox (public_id, organization_id, topic, message, attributes)
VALUES (@public_id, @organization_id, @topic, @message, @attributes);
