---
"dashboard": minor
---

Organization admins can give themselves temporary access where a page refuses it. Wherever the dashboard says a permission is missing, an admin now sees **Grant myself temporary access** for permissions that can be given for a limited time: they pick 30 minutes, 2 hours, 4 hours, end of day or 24 hours, give a reason, and the access applies at once and is recorded in the audit log as self-granted. Members still ask an admin. Risk Events, the transcript sheet, Agent Sessions, skill insights and Watchdog evidence offer this where `chat:read` is missing, for one session or for every session. While any temporary access is active, a **Temporary access** panel floats in the bottom-right corner of every page, listing each grant with what it covers, when it ends and the time left, and a Revoke button that asks once. The Authorization Challenges view shows denials answered by temporary access as **Temporary** until the access ends.
