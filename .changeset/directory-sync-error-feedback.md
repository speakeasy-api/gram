---
"server": patch
"dashboard": patch
---

Identity provider setup failures now say what went wrong and what to do next. A refused WorkOS admin portal link used to come back as a blanket 500 reading "failed to generate WorkOS admin portal link"; `organizations.generateWorkOSAdminPortalLink` now classifies the WorkOS outcome — organization missing, request rejected, credentials refused, rate limited, upstream down, unreachable — and names the intent and the next step, without forwarding WorkOS's response body. A 200 carrying a blank link is rejected too, rather than handing the dashboard nothing to open. In the setup wizard, the three admin portal round trips share one hook, a status check that failed is no longer reported as setup WorkOS has not seen, and a setup status that cannot be read shows an alert with a retry rather than a confident "not set up".
