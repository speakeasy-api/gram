---
"dashboard": patch
---

Keep project favorites when a logged-out visit is sent to the login page. The session-expiry cleanup now snapshots theme and favorites after auth confirms there is no session, instead of deleting them because that document was never classified.
