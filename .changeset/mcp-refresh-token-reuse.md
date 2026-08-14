---
"server": patch
---

Stop hosted MCP OAuth from rotating refresh tokens on `/token`. MCP clients retry the refresh grant and share one credential store across processes; consuming the refresh token on first use was returning `invalid_grant` ("already used") and forcing a daily re-login. Access tokens still rotate, authorization lifetime is unchanged, and `/revoke` still invalidates the grant.
