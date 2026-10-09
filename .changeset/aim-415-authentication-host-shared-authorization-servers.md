---
"server": minor
---

A shared authorization server whose issuer is on the authentication host, pinned there or derived for an issuer that opts in with `use_authentication_host`, is now served on that host, giving platforms such as Claude Tag a token endpoint on a host that carries no MCP traffic. Until authorization, consent, and registration reach the authentication host, it serves only its metadata, token, and revocation endpoints and accepts only the workload grant, while MCP clients keep signing in through the per-endpoint authorization server.
