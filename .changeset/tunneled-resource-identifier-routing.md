---
"server": minor
---

Tunneled MCP servers can record an RFC 9728 resource identifier (never dialed), which gateway consent stamps as the RFC 8707 resource on the grants its members mint. A tunneled backend is routed by its own derived provider issuer, accepting that grant when it is unqualified or names the recorded identifier — never a credential minted through another authorization server, since a tunnel's dial target is decoupled from the resource its identifier claims. Remote backends continue to match on recorded resource across the session's credentials. The lone-token routing fallback is removed on both surfaces: an unmatched credential is never forwarded.
