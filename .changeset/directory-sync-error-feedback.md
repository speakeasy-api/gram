---
"server": patch
"dashboard": patch
---

Directory Sync setup failures now say what went wrong and what to do next. A refused WorkOS admin portal link used to come back as a blanket 500 reading "failed to generate WorkOS admin portal link"; `organizations.generateWorkOSAdminPortalLink` now classifies the WorkOS outcome — organization missing, request rejected, credentials refused, rate limited, upstream down, unreachable — and names the intent and the next step, without forwarding WorkOS's response body. In the setup wizard, a portal that never opens (a blocked pop-up) reports itself instead of leaving the click silent, a status check that failed is no longer reported as "not detected yet", Directory Sync is gated on a verified domain the way single sign-on already is, and a setup status that cannot be read shows an alert with a retry rather than a confident "not connected".
