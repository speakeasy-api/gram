---
"dashboard": patch
"server": patch
---

The identity chaining control on a remote MCP server's authentication settings now appears for an organization user session issuer that Speakeasy's own issuer can vouch for, not only one that trusts an external identity provider sign-in client. Anyone who can read the organization sees it; organization admins can use **Add Speakeasy** to append Speakeasy to the vendor's EMA provider order, behind the `ema-providers` rollout. The new `organizationRemoteSessionIssuers.addSpeakeasyEMAProvider` endpoint backs it. It only acts for a sign-in that trusts no external identity provider, never removes or reorders providers, and refuses to replace a vendor's default order while another server reaches that vendor through an external identity provider. Platform MCP guidance points administrators to the control.
