---
"server": minor
---

Tunneled MCP servers can record an RFC 9728 resource identifier (never dialed) that is stamped as the RFC 8707 resource on grants at consent time and routed by exact match on both the direct and gateway serving surfaces. The lone-token routing fallback is removed: an unmatched credential is never forwarded, and a tunneled backend without an identifier routes by its own derived issuer identity or calls anonymously.
