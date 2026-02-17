---
"@gram-ai/elements": patch
---

Auto-refresh expired session tokens before chat requests. Adds JWT expiry detection with a configurable buffer, deduplicates concurrent refresh calls, and falls back to the stale token on failure so the server can decide via 401. Controlled by the new `refreshSession` prop (defaults to `true`, skipped for static sessions and replays).
