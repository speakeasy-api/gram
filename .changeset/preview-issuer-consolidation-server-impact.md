---
"server": minor
"dashboard": patch
---

Consolidating one authorization server into another now shows how each affected MCP server's OAuth clients would change before you confirm, and keeps each server's working OAuth client explicit through the move. The preview runs the consolidation and rolls it back, so it reports exactly what confirming would do: servers that would start or stop asking people to connect an account, switch providers, or keep their current client. It blocks the consolidation when a server would end up with two OAuth clients for one provider, or with a client its upstream does not accept. Consolidation now carries over only the OAuth clients something still signs in through, never a deleted one, and lists the rest. Previewing a consolidation now requires organization admin access, because it takes the same locks a consolidation does.
