-- name: InsertOutboxEntry :one
-- Inserts a new outbox event for an organization and returns identifiers
-- needed for downstream relay/signal coordination.
INSERT INTO outbox (organization_id, event_type, payload)
VALUES (@organization_id, @event_type, @payload)
RETURNING id, created_at;

-- name: BulkInsertOutboxEntries :copyfrom
INSERT INTO outbox (organization_id, event_type, payload)
VALUES (@organization_id, @event_type, @payload);
