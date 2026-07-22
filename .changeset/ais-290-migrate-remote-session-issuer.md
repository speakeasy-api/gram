---
"server": minor
"dashboard": patch
---

Add organizationRemoteSessionIssuers.migrate API and UI to consolidate two remote identity providers that point at the same upstream authorization server, re-pointing the source's clients onto the target and soft-deleting the source without forcing anyone to re-authenticate
