---
"dashboard": patch
---

Link the SSO and Directory Sync (SCIM) "Configure" buttons on the org Identity page to the in-product setup wizard (`/setup?step=connect-idp` and `/setup?step=directory-sync`) when no connection has been set up yet, instead of bouncing admins straight to the WorkOS admin portal. Once a connection exists, the buttons continue to open the WorkOS portal to manage it. The entitled-org configure buttons no longer emit `identity_provider_interest` PostHog tracking events; the non-entitled upsell button still does (it backs the "our team has been contacted" notification).
