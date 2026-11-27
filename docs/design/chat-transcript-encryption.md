# Chat transcript encryption (DNO-1573)

A message body is six `chat_messages` columns: `content`, `content_raw`, `tool_calls`, `tool_outcome_notes`, `tool_call_summaries` and `storage_error`. `chat_message_transcripts` holds the body encrypted with the organization's key. `transcriptstore.Transcripts` is the only way to read or change a body.

## Modes

Each project has a mode, set by `GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS`:

- **Plaintext:** plaintext columns only. No transcript is read or written.
- **Shadow:** plaintext and transcript. Reads and updates decrypt the transcript and compare it with the plaintext row. If they agree, the transcript is used; if not, the error is logged with each differing field's size, never its contents, and the plaintext wins.
- **Encrypted:** transcript only, with empty plaintext columns; a missing transcript is an error. Not configurable yet.

## Writes

Writers call `Prepare` before `BEGIN`, so no KMS call runs inside a transaction. `Prepare` caches only the active key; a transcript under an older key needs that key cached.

- **New message:** `Seal` locks the row, encrypts it and upserts the transcript.
- **Existing message:** `UpdateBody` locks the row, reads the body, applies the change in Go, writes the plaintext, transcript, metadata and tool-call IDs, then runs a sanity check that decrypts what it stored.

A failure rolls back the whole transaction, so a transcript is never stale.

## Legacy transcripts and backfill

Transcripts sealed before this design carry an unkeyed `source_hash`, which is plaintext-equivalent. They read as missing until `Backfill` reseals them. Run one full backfill after deploying.

## Metrics

`chat.transcript_shadow.read.messages` and `chat.transcript_shadow.update.checks` count every outcome.
