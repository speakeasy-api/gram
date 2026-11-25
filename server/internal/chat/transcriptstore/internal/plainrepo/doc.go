// Package plainrepo holds every reader query that reads the plaintext
// transcript columns of chat_messages (content, content_raw, tool_calls,
// tool_outcome_notes, tool_call_summaries, storage_error). It lives inside
// transcriptstore/internal, so the Go compiler refuses to let any other
// package call one. Each read LEFT JOINs the message's encrypted shadow when
// @open_shadows is true (the project keeps shadows), returning the ciphertext
// and whether its source hash still matches in the same round trip. When it
// is false the join is skipped by a one-time filter and costs nothing.
package plainrepo
