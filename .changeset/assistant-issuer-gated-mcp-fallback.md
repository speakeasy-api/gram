---
"server": patch
---

Issuer-gated MCP servers now accept an assistant-runtime JWT and use the assistant owner's linked upstream account, so the runtime can call `/mcp/{slug}` without re-prompting for login. Requests with no linked upstream still return a 401 + WWW-Authenticate as before.
