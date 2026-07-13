---
"server": patch
---

Stop the chat session list visibility check from recording an authz challenge. Listing sessions probes `chat:read` only to decide whether the caller sees all sessions or just their own; a member without the grant is the normal case, not a denial. Logging it as one polluted the access diagnostics with spurious `chat:read` denials (the insights dock lists chats on every page load), making it look like `chat:read` was required to view unrelated pages such as the Cost dashboard.
