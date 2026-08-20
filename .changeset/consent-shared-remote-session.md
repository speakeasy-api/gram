---
"server": patch
---

The MCP consent page now shows a connected upstream grant even when a different identity provider originally created the row, including after that provider is removed. Consent disconnect, auto-refresh, explicit refresh, scheduled keepalive, and user-session revocation follow the live client binding rather than the minting issuer, so they no longer miss the tokens the MCP runtime was already sending.
