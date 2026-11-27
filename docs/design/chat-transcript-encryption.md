# Chat transcript encryption (DNO-1573)

Six `chat_messages` columns are protected fields: `content`, `content_raw`, `tool_calls`, `tool_outcome_notes`, `tool_call_summaries` and `storage_error`. `chat_message_transcripts` holds them encrypted with the organization's key. `transcriptstore.Transcripts` is their only writer and serves chat loads; other reads stay plaintext until rollout step 5.

## Modes

- **Plaintext:** no transcript.
- **Shadow** (`GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS`): plaintext and transcript. Reads and updates decrypt the transcript and compare it with the plaintext; a mismatch is logged by field size, never contents, and plaintext wins. `Backfill` reseals from plaintext; run it before re-adding a removed project.
- **Encrypted:** transcript only, plaintext columns empty; inserts never write plaintext; a missing transcript is an error. Not configurable yet.

## Reads

Every query that reads the protected fields lives in `transcriptstore/internal/plainrepo`, so only `Transcripts` can call it. Each one joins the message's transcript, which `Transcripts` opens with the rules above. The `no-plaintext-transcript-read` glint rule rejects a read, an insert or a raw update of them anywhere else.

## Search

SQL cannot match ciphertext, so for a listed project `SearchChat` (`chat.load` with `query`) and `SearchPlatformChats` (`search_chats`) scan messages in keyset chunks of 200. A chunk returns transcripts, not plaintext bodies; Go opens each and matches with the SQL `ILIKE` rules, reading JSON as `jsonb::text` prints it. A message without a transcript is matched on plaintext. The rows a search returns are compared with plaintext like any read. `chat.load` scans one chunk per call and returns a cursor; `search_chats` scans up to 1,000 messages and reports `scan_incomplete`.

## Writes

Writers open their transaction with `Begin`, which loads the key before `BEGIN`, so KMS runs outside the transaction; only the active key is cached. `transcriptstore` runs every chat message insert, and `UpdateChatMessage` every change. Both use one protector: it reads the protected fields once, writes them (empty in encrypted mode) with their metadata in the same statement, and, outside plaintext mode, seals that same value, all in one transaction. A failure rolls back; a sanity-check mismatch is logged.

Legacy transcripts with an unkeyed `source_hash` read as missing until `Backfill` reseals them.

`chat.transcript_shadow.read.messages` and `.update.checks` count every outcome.

## Rollout

1. Deploy with an empty allowlist.
2. Add one internal project to the allowlist and run a full backfill.
3. Watch `stale`, `hydrate_mismatch`, `sanity_mismatch` and `error` for a week; each must stay at zero.
4. Widen the allowlist project by project, backfilling each.
5. Move the remaining plaintext SQL (counts, lookups) behind `Transcripts`.
6. Make encrypted mode configurable and teach backfill to empty plaintext; switch one project, then the rest.
7. Drop `source_hash` once no row carries it.
