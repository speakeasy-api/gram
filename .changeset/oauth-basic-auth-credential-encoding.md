---
"server": patch
---

Fix OAuth token exchanges failing with invalid_client against providers that strictly decode HTTP Basic credentials (e.g. Snowflake): client id and secret are now form-urlencoded before being placed in the Authorization header, per RFC 6749 §2.3.1.
