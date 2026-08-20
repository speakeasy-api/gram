---
"server": patch
---

The MCP consent page now shows a connected upstream grant even when a different identity provider on the same project originally created the row. Consent disconnect, auto-refresh, and user-session revocation follow the same project-scoped key, so they no longer miss the live tokens the MCP runtime was already sending.
