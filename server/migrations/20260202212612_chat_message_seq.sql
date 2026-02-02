-- Add seq column without constraints first
ALTER TABLE "chat_messages" ADD COLUMN "seq" bigint;

-- Backfill seq values based on id order (UUIDv7 is chronologically sorted)
WITH ranked_messages AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY id) AS row_num
  FROM chat_messages
)
UPDATE chat_messages
SET seq = ranked_messages.row_num
FROM ranked_messages
WHERE chat_messages.id = ranked_messages.id;

-- Now add the identity generator and constraints
CREATE SEQUENCE IF NOT EXISTS chat_messages_seq_seq;
ALTER TABLE "chat_messages" ALTER COLUMN "seq" SET DEFAULT nextval('chat_messages_seq_seq');
ALTER TABLE "chat_messages" ALTER COLUMN "seq" SET NOT NULL;
ALTER TABLE "chat_messages" ADD CONSTRAINT "chat_messages_seq_key" UNIQUE ("seq");

-- Update the sequence to start after the highest current value
SELECT setval('chat_messages_seq_seq', (SELECT GREATEST(COALESCE(MAX(seq), 0), 1) FROM chat_messages));
