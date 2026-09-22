---
"dashboard": patch
"server": patch
---

Okta readiness dashboard: gate the applications snapshot on a clean verification and explain a degraded connection inline, scroll to the connection card after verifying, show when a degraded check ran, confirm before resetting a Cross App Access confirmation, surface confirmed rows under the Needs action filter, fit the readiness table at 1440px, disambiguate duplicate app instances, say Speakeasy consistently in the console checklist, move Sync now feedback to a toast, and fold the Okta page into the Identity page as concern tabs (`identity?tab=sso|provider|applications|cross-app-access`; `/okta` and `identity?tab=okta` redirect there) with a vendor-neutral provider picker. The console checklist is now two groups, Connect and Cross App Access setup, with the steps a verification can observe ticked automatically, the per-app steps left to the Cross App Access tab, and the agent credential step deferred until the token exchange consumes it.
