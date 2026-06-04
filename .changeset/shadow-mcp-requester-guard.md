---
"server": patch
---

Fix shadow MCP access requests failing with a 403 ("different requester") when the request link was minted for an agent-reported identity that differs from the authenticated dashboard user (multi-domain orgs, duplicate accounts, or a shared block link). `access.shadowMcp.requests.create` no longer gates on the token's requester; org-match and project-membership checks remain, and approval stays org-admin gated.
