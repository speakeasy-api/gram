# Chat transcript encryption (DNO-1573)

A message body is six `chat_messages` columns: `content`, `content_raw`, `tool_calls`, `tool_outcome_notes`, `tool_call_summaries` and `storage_error`. `chat_message_transcripts` holds it encrypted with the organization's key. Only `transcriptstore.Transcripts` reads or changes a body.

## Modes

- **Plaintext:** no transcript.
- **Shadow** (`GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS`): both copies. Reads and updates decrypt the transcript and compare it with the plaintext; a mismatch is logged by field size, never contents, and plaintext wins, as when there is no transcript. Backfill before re-adding a removed project.
- **Encrypted:** transcript only, plaintext columns empty; a missing transcript is an error. Not configurable yet.

## Reads

Every query that reads the body columns lives in `transcriptstore/internal/plainrepo`, so only `Transcripts` can call it. Each one joins the message's transcript, which `Transcripts` opens with the rules above. The `no-plaintext-transcript-read` glint rule rejects a body read, a body insert or a raw body update anywhere else.

`streams` only reads, so it opens transcripts without a writer or key provisioning. A process without organization encryption gets read-only `Transcripts`: a read for a listed project logs an error and counts `chat.transcript_shadow.plaintext_reads` with `gram.outcome:unopened`, and fails in local development. A canary test edits plaintext behind a current transcript and checks that every reader counts it as stale.

## Writes

Writers call `Prepare` before `BEGIN`, so KMS runs outside the transaction; only the active key is cached. `Seal` encrypts a new message. `UpdateMessage` locks the row and transcript, applies the change in Go and writes every copy. A failure rolls back; a sanity-check mismatch is logged.

Transcripts with the legacy unkeyed `source_hash` are plaintext-equivalent and read as missing until `Backfill` reseals them.

`chat.transcript_shadow.read.messages` and `.update.checks` count every outcome.

## Rollout

1. Deploy with an empty allowlist.
2. Add one internal project to the allowlist and run a full backfill.
3. Watch `stale`, `hydrate_mismatch`, `sanity_mismatch` and `error` for a week; each must stay at zero.
4. Widen the allowlist project by project, backfilling each.
5. Move the remaining plaintext SQL (search, counts, lookups) behind `Transcripts`.
6. Make encrypted mode configurable and teach backfill to empty plaintext; switch one project, then the rest.
7. Drop `source_hash` once no row carries it.
