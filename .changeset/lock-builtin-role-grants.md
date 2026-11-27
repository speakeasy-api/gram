---
"server": minor
---

Expose `locked_grants` on `access.Role` and reject permission writes targeting locked Speakeasy built-in roles across role updates, role deletion, per-server access rules, temporary grants, and Platform MCP role mutations.
