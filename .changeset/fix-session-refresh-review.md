---
"@gram-ai/elements": patch
---

Auto-refresh expired session tokens before chat requests. Adds JWT expiry detection with a 30s buffer, deduplicates concurrent refresh calls via `fetchQuery`, and falls back to the stale token on failure so the server can decide via 401. Refresh is automatic for non-static sessions and skipped during replays.
