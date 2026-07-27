---
"server": patch
---

MCP servers backed by an external OAuth authorization server now serve RFC 8414 authorization-server metadata whose `issuer` matches the Gram resource URL, so spec-compliant OAuth clients no longer reject the document.
