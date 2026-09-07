---
"server": patch
"dashboard": patch
---

Limit dashboard session tokens to ten minutes and silently renew them through a cookie-only refresh credential with a 72-hour inactivity window. Existing sessions require a one-time sign-in after deployment.
