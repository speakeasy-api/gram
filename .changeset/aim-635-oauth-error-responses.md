---
"server": patch
---

Speakeasy OAuth authorization and browser sign-in errors now return HTTP 400 instead of 401, with their existing descriptions. Invalid client metadata at MCP authorization endpoints returns `invalid_client`. Token and revocation client authentication failures return 401 with a Basic challenge when HTTP Basic was attempted, and 400 otherwise. Admin MCP token requests with duplicate or mixed client credentials return 400 `invalid_request`. Dependency outages during authorization authority checks, consent state lookup, client lookup, and identity-provider code exchange return HTTP 503 instead of client errors.
