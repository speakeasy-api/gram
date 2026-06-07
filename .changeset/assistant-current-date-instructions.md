---
"server": patch
---

Give the managed (Project) Assistant temporal grounding by stamping each dashboard turn with its timestamp. `dashboardAdapter.DecodeTurn` now adds a `Timestamp: <RFC3339 UTC>` line to the turn's `<message-context>` envelope, sourced from the event's immutable `created_at`. This restores the relative-time anchoring the old AI Insights sidebar had ("errors since Monday") but does it per-turn and append-only — it rides on the user message instead of the cached system prompt, so it stays fresh across long-lived sessions without busting the prompt cache, and re-decoding on retry/replay is byte-stable.
