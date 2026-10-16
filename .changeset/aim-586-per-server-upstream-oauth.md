---
"server": minor
---

MCP servers that share a sign-in can each use their own upstream OAuth client. Each server, and each gateway member, signs in through the client configured for it instead of a provider shared by every server on the sign-in. The consent screen offers each server only its own clients. Connections made before a server was configured are asked to reconnect, never handed another server's client.
