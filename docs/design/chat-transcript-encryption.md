# Chat transcript encryption (DNO-1573)

Six `chat_messages` columns are protected fields: `content`, `content_raw`, `tool_calls`, `tool_outcome_notes`, `tool_call_summaries` and `storage_error`. `chat_message_transcripts` holds them encrypted with the organization's key. Only `transcriptstore.Transcripts` reads or changes them.

## Modes

- **Plaintext:** no transcript.
- **Shadow** (`GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS`): plaintext and transcript. Reads and updates decrypt the transcript and compare it with the plaintext; a mismatch is logged by field size, never contents, and plaintext wins, as when there is no transcript. `Backfill` reseals from plaintext; run it before re-adding a removed project.
- **Encrypted:** transcript only, plaintext columns empty; a missing transcript is an error, and an existing one is never resealed. Not configurable yet.

## Reads

Every query that reads the protected fields lives in `transcriptstore/internal/plainrepo`, so only `Transcripts` can call it. Each one joins the message's transcript, which `Transcripts` opens with the rules above. Counts, size caps, the sigint history budget, the empty-prompt filter and the hooks tool-call lookup read the plaintext-free metadata that sealing writes (`tool_call_count`, `*_bytes`, `chat_message_tool_call_ids`), and fall back to plaintext only for a message never sealed. The `no-plaintext-transcript-read` glint rule rejects a read, an insert or a raw update of them anywhere else.

## Writes

Writers call `Prepare` before `BEGIN`, so KMS runs outside the transaction; only the active key is cached. `Seal` encrypts a new message. `UpdateChatMessage` locks the row and transcript, applies the change in Go and writes every copy. A failure rolls back; a sanity-check mismatch is logged.

Transcripts with the legacy unkeyed `source_hash` are plaintext-equivalent and read as missing until `Backfill` reseals them.

`chat.transcript_shadow.read.messages` and `.update.checks` count every outcome.

## Rollout

1. Deploy with an empty allowlist.
2. Add one internal project to the allowlist and run a full backfill.
3. Watch `stale`, `hydrate_mismatch`, `sanity_mismatch` and `error` for a week; each must stay at zero.
4. Widen the allowlist project by project, backfilling each.
5. Move search (`ILIKE`), the last plaintext SQL, behind `Transcripts`.
6. Make encrypted mode configurable and teach backfill to empty plaintext; switch one project, then the rest.
7. Drop `source_hash` once no row carries it.
