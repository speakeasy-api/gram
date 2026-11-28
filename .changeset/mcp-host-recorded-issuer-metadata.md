---
"server": patch
---

MCP clients that signed in before their server's MCP sign-in moved to the authentication host, including servers moved or consolidated onto a shared authorization server that uses it, can refresh again without clearing stored credentials. The MCP host keeps serving authorization server metadata under the issuer those clients recorded, and its token and revocation endpoints accept client assertions signed for that issuer. Clients whose registration did not move with the server get `invalid_client` and must sign in again.
