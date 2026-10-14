---
"dashboard": patch
"server": patch
---

The identity chaining control on a remote MCP server's authentication settings now appears for an organization user session issuer that Speakeasy's own issuer can vouch for, not only one that trusts an external identity provider sign-in client. Organization admins can add or remove Speakeasy in the vendor's EMA provider order from the same control, behind the `ema-providers` rollout. Platform MCP guidance now sends administrators to that control instead of the management API.
