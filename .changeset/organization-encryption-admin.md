---
"server": minor
---

Staff can now move organizations' OAuth client secrets and session tokens from the platform key to their own encryption keys from the admin dashboard. They can enroll organizations one at a time or in batches, roll back, start a backfill, and finalize, which clears the platform-key copies after an acknowledged confirmation. Finalize is refused while the organization's key is suspended.
