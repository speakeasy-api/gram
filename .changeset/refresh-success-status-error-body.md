---
"server": patch
---

Read OAuth error bodies on 2xx upstream token responses so a dead refresh grant reported that way (GitHub's `bad_refresh_token`) clears the stored refresh token instead of being retried indefinitely.
