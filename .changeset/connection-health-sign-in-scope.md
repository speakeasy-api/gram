---
"server": patch
"dashboard": patch
---

The MCP authentication provider's Overview now covers signing in to Speakeasy only. Its chart and sign-in activity count attempts that ended at Speakeasy's sign-in or sign-in renewal, and their links open Connection Logs filtered to the same steps, so the totals match. Problems connecting to a service show on that server's page. `getSignInSummary` takes optional `steps`, matching the Connection Logs step filter; without it the summary counts what it did before.
