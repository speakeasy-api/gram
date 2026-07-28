---
"server": patch
---

Stop asking MCP users to reconnect when several of their requests refresh an upstream token at the same time. Concurrent resolves for one subject all presented the same stored refresh token, so a provider that rotates single-use tokens honoured the first and rejected the rest, and every rejected caller was told to reconnect a session the winner had already repaired. Refresh is now single-flighted per (subject, remote session client) with a short Redis lock — losers wait for the winner's write and adopt its token instead of calling the provider — and the write itself is a compare-and-swap on `updated_at`, so a losing writer can no longer persist a refresh token the provider has already consumed.
