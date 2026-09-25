---
"dashboard": patch
"server": patch
---

Report a Google Ads conversion when someone signs up for the platform. The server marks the post-signup redirect with `signed_up=1`, and the dashboard fires the `platform_signup` gtag event once (also from the register page) when a build carries `GRAM_GOOGLE_TAG_ID`.
