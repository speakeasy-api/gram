---
"dashboard": patch
"server": patch
---

Redirect to the login page after account-menu logout. Chromium never settles a fetch whose response includes Clear-Site-Data: "cache", so that directive is omitted. The page still navigates if logout rejects or stalls.
