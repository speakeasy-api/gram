---
"server": minor
"dashboard": patch
---

`remoteSessions.commitServerIdentityConfiguration` accepts an optional `registration_method` in auto client mode. `cimd`, the default, prefers a Client ID Metadata Document and falls back to dynamic client registration; `dcr` always registers dynamically.
