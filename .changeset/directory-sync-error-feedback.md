---
"server": patch
"dashboard": patch
---

Identity provider setup failures now tell the admin what to do next. A refused WorkOS admin portal link used to come back as a blanket 500 reading "failed to generate WorkOS admin portal link"; `organizations.generateWorkOSAdminPortalLink` now answers with the admin's next step — wait and retry, slow down, verify a domain, or contact support — chosen from the upstream outcome, with the cause kept in the log and the span. The messages describe the admin's setup rather than our dependency on WorkOS, since which upstream refused and why is a distinction only Speakeasy can act on. In the setup wizard, the three admin portal round trips share one hook, a status check that failed is no longer reported as setup WorkOS has not seen, and a setup status that cannot be read shows an alert with a retry rather than a confident "not set up".
