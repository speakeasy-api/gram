# Chat transcript encryption (DNO-1573)

Six `chat_messages` columns are protected fields: `content`, `content_raw`, `tool_calls`, `tool_outcome_notes`, `tool_call_summaries` and `storage_error`. `chat_message_transcripts` holds them encrypted with the organization's key. `transcriptstore.Transcripts` is their only writer and serves chat loads; other reads stay plaintext until rollout step 5.

## Modes

- **Plaintext:** no transcript.
- **Shadow** (`GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS`): plaintext and transcript. Reads and updates decrypt the transcript and compare it with the plaintext; a mismatch is logged by field size, never contents, and plaintext wins. `Backfill` reseals from plaintext; run it before re-adding a removed project.
- **Encrypted:** transcript only, plaintext columns empty; inserts never write plaintext; a missing transcript is an error. Not configurable yet.

## Writes

Writers open their transaction with `InTx`, which loads the key before `BEGIN`, so KMS runs outside the transaction; only the active key is cached. `transcriptstore` runs every chat message insert, and `UpdateChatMessage` every change. Both use one protector: it reads the protected fields once, writes them (empty in encrypted mode) with their metadata in the same statement, and, outside plaintext mode, seals that same value, all in one transaction. A failure rolls back; a sanity-check mismatch is logged.

Legacy transcripts with an unkeyed `source_hash` read as missing until `Backfill` reseals them.

`chat.transcript_shadow.read.messages` and `.update.checks` count every outcome.

## Rollout

1. Deploy with an empty allowlist.
2. Add one internal project to the allowlist and run a full backfill.
3. Watch `stale`, `hydrate_mismatch`, `sanity_mismatch` and `error` for a week; each must stay at zero.
4. Widen the allowlist project by project, backfilling each.
5. Move the remaining plaintext SQL (search, counts, lookups) behind `Transcripts`.
6. Make encrypted mode configurable and teach backfill to empty plaintext; switch one project, then the rest.
7. Drop `source_hash` once no row carries it.
