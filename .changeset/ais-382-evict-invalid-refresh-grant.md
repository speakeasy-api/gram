---
"server": patch
---

Stop remote-session MCP requests from looping on a dead upstream refresh token. When an upstream token endpoint returns a definitive RFC 6749 `invalid_grant`, the stored session is now soft-deleted (compare-and-swapped on `updated_at` so a concurrent refresh or re-link is never clobbered) instead of being retried on every request. The next request establishes a fresh upstream session rather than replaying the dead grant.
