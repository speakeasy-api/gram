# Chat transcript encryption (DNO-1573)

A message body is six `chat_messages` columns: `content`, `content_raw`, `tool_calls`, `tool_outcome_notes`, `tool_call_summaries` and `storage_error`. `chat_message_transcripts` holds it encrypted with the organization's key. Only `transcriptstore.Transcripts` reads or changes a body.

## Modes

- **Plaintext:** no transcript.
- **Shadow** (`GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS`): both copies. Reads and updates decrypt the transcript and compare it with the plaintext; a mismatch is logged by field size, never contents, and plaintext wins.
- **Encrypted:** transcript only, plaintext columns empty; a missing transcript is an error. Not configurable yet.

## Writes

Writers call `Prepare` before `BEGIN`, so KMS runs outside the transaction; only the active key is cached. `Seal` encrypts a new message. `UpdateBody` locks the row, applies the change in Go, writes every copy and sanity-checks the stored transcript. Any failure rolls back.

Transcripts with the legacy unkeyed `source_hash` are plaintext-equivalent and read as missing until `Backfill` reseals them.

`chat.transcript_shadow.read.messages` and `.update.checks` count every outcome.

## Rollout

1. Deploy with an empty allowlist.
2. Add one internal project to the allowlist and run a full backfill.
3. Watch the mismatch, `stale`, `sanity_mismatch` and `error` outcomes for a week; each must stay at zero.
4. Widen the allowlist project by project, backfilling each.
5. Move the remaining plaintext SQL (search, counts, lookups) behind `Transcripts`.
6. Make encrypted mode configurable, extend backfill to empty existing plaintext, then switch one project, then the rest.
7. Drop `source_hash` once no row carries it.
