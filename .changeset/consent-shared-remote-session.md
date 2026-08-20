---
"server": patch
---

The MCP consent page now shows a connected upstream grant even when a different identity provider originally created the row, including after that provider is removed. Consent disconnect, auto-refresh, and user-session revocation follow the same client-scoped key, so they no longer miss the live tokens the MCP runtime was already sending.
