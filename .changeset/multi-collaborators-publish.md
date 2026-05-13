---
"dashboard": minor
"server": minor
---

Allow adding multiple GitHub collaborators when publishing plugins to a marketplace. The publish dialog accepts a list of usernames as chips, and the `publishPlugins` API now takes `github_usernames` (array) instead of `github_username` (string).
