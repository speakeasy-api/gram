---
"server": patch
---

Connecting an upstream service from the MCP consent page on an extra platform host (such as `ai.speakeasy.com`) now returns to that host after the upstream login, instead of failing with "authn challenge state does not match this MCP server". The disabled "Give access" button on the consent page also no longer renders black on black while hovered or focused.
