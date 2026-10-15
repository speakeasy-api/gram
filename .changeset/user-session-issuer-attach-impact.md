---
"server": minor
"dashboard": minor
---

Warn before changing the user session issuer of an MCP server that already has one. Its connected clients must sign in again, and the new `userSessionIssuers.getAttachImpact` preview also says when the authorization server clients are sent to changes, so anything configured with it must be updated. MCP server settings now show the issuer URL, token endpoint, and resource of a shared authorization server, and the issuer picker marks issuers that share one.
