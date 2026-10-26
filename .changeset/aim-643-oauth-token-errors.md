---
"server": patch
---

OAuth token endpoints now distinguish infrastructure failures from invalid clients and expired or revoked grants, so clients can retain their registration and recover appropriately. MCP token and revocation failures use OAuth JSON responses with cache-prevention headers, and missing grant types return `invalid_request`.
