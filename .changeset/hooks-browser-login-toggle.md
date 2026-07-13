---
"server": minor
"dashboard": minor
---

Hook plugin browser sign-in is now opt-in per organization. By default, published plugins never open a browser: they authenticate with explicitly configured credentials, a previously cached key, or the organization-wide key, and the login helper prints manual setup instructions instead. Organization admins can re-enable the interactive browser sign-in from the org settings page.
