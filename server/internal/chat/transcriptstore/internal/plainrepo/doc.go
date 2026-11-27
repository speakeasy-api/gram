// Package plainrepo holds the transcript store's sqlc queries that read the
// plaintext transcript columns of chat_messages (content, content_raw,
// tool_calls, tool_outcome_notes, tool_call_summaries, storage_error). Known
// reads outside it: chat/repo ListChatMessages (tests and fixtures only, which
// the no-plaintext-transcript-read lint rule enforces), the store's own capture
// and backfill queries in transcriptstore/queries.sql, and raw SQL in the ops
// tools under server/cmd/tools. It lives inside
// transcriptstore/internal, so the Go compiler refuses to let any other
// package call one. Each read LEFT JOINs the message's encrypted shadow when
// @open_shadows is true (the project keeps shadows), returning the ciphertext
// and whether its source hash still matches in the same round trip. When it
// is false the join is skipped by a one-time filter and costs nothing.
package plainrepo
