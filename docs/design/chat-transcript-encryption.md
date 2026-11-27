# Chat transcript encryption (DNO-1573)

A message body is six `chat_messages` columns: `content`, `content_raw`, `tool_calls`, `tool_outcome_notes`, `tool_call_summaries` and `storage_error`. `chat_message_transcripts` holds it encrypted with the organization's key. Only `transcriptstore.Transcripts` reads or changes a body.

## Modes

- **Plaintext:** no transcript.
- **Shadow** (`GRAM_CHAT_TRANSCRIPT_SHADOW_PROJECT_IDS`): both copies. Reads and updates decrypt the transcript and compare it with the plaintext; a mismatch is logged by field size, never contents, and plaintext wins, as when there is no transcript. Backfill before re-adding a removed project.
- **Encrypted:** transcript only, plaintext columns empty; a missing transcript is an error. Not configurable yet.

## Writes

Writers call `Prepare` before `BEGIN`, so KMS runs outside the transaction; only the active key is cached. `Seal` encrypts a new message. `UpdateMessage` locks the row and transcript, applies the change in Go and writes every copy. A failure rolls back; a sanity-check mismatch is logged.

Transcripts with the legacy unkeyed `source_hash` are plaintext-equivalent and read as missing until `Backfill` reseals them.

`chat.transcript_shadow.read.messages` and `.update.checks` count every outcome.

## Backfill

`gram transcript-shadow-backfill --project-id <PROJECT_ID>` reports coverage (`current`, `waiting`, `unattributed`). Add `--apply --confirm-environment "$GRAM_ENVIRONMENT"` to seal. Run it from a server pod: it builds the same `Transcripts` as server startup and seals through `Transcripts.Backfill`, so each message gets the same metadata and `chat_message_tool_call_ids` rows as a live `Seal`.

- It refuses a project off the allowlist (`ErrNotEnabled`): that project seals nothing new and its updates leave transcripts stale.
- Each page (default 200, at most 500) commits whole or not at all and reseals every message in it, which clears `source_hash`.
- A run that stops early prints a `--after` cursor to resume from. Start each new sweep from the zero cursor to cover messages inserted below the last one.
- `waiting` counts messages with no transcript or only a legacy one; it does not authenticate ciphertext. `unattributed` messages carry a project other than their chat's; backfill skips them until their ownership is repaired. A project is covered at `waiting=0` and `unattributed=0`.

## Rollout

1. Deploy with an empty allowlist.
2. Add one internal project to the allowlist and run a full backfill.
3. Watch `stale`, `hydrate_mismatch`, `sanity_mismatch` and `error` for a week; each must stay at zero.
4. Widen the allowlist project by project, backfilling each.
5. Move the remaining plaintext SQL (search, counts, lookups) behind `Transcripts`.
6. Make encrypted mode configurable and teach backfill to empty plaintext; switch one project, then the rest.
7. Drop `source_hash` once no row carries it.
