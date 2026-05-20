---
"server": patch
---

Public-MCP `/authorize` now stamps the caller's Gram user on the resulting `remote_sessions` row when one of a Gram session cookie / `Gram-Session` header / Bearer user-session JWT is present, instead of always minting a fresh anonymous subject. Unauthenticated callers (third-party MCP clients) keep their per-session anonymous subject — behaviour unchanged.
