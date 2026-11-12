---
"server": patch
---

When Speakeasy is the company sign-in provider and won't vouch for an account (for example, the person isn't a member, was deprovisioned, or used a sign-in method the organization doesn't allow), connection health and connection log exports now record it as something an admin has to fix, instead of asking the person to sign in again. Company sign-ins that need renewing are now recorded as such on MCP requests, too.
