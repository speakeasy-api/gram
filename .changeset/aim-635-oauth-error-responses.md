---
"server": patch
---

Speakeasy OAuth authorization and browser sign-in errors now return HTTP 400 instead of 401, with their existing descriptions. Invalid client metadata at MCP authorization endpoints returns `invalid_client`. Token and revocation client authentication failures return 401 with a matching challenge when authentication was attempted through the Authorization header, and 400 otherwise. Token and revocation requests with duplicate credentials or multiple authentication methods return 400 `invalid_request` before authentication. Dependency outages during authorization authority checks, consent state lookup, client lookup, and identity-provider code exchange return HTTP 503 instead of client errors.
