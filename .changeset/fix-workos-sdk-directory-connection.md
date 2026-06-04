---
"server": patch
---

Fix `organizations.getOnboardingStatus` returning 500 in production by switching the WorkOS connection/directory lookups to the official WorkOS Go SDK (`sso.Client`, `directorysync.Client`). The previous raw-HTTP wrapper used the wrong path `/directory_sync/directories` (the correct WorkOS endpoint is `/directories`), which the type system could not catch.
