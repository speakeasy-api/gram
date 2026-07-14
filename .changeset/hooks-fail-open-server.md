---
"server": minor
---

Add the org-level `hooks_fail_open` product feature (DNO-497): org admins choose whether agent hooks fail open or fail closed (the default) when the Speakeasy control plane is unreachable or erroring and no policy verdict can be obtained. The setting is delivered to hook senders as an `org_settings` entry in every authenticated `hooks.ingest` response's effects map, and toggling it records an `organization:hooks_fail_open_enabled|disabled` audit event. The speakeasy-hooks binary caches the last server-confirmed value next to its credential cache and consults it only on the unreachable/5xx branch of verdict resolution — explicit denies, 4xx responses, and the 401/403 credential ratchet keep failing closed regardless. The cached posture expires after 14 days without server confirmation (reverting to fail closed), and successful exchanges re-stamp an unchanged value daily so actively syncing machines never age out.
