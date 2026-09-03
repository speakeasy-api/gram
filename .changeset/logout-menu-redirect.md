---
"dashboard": patch
"server": patch
---

Redirect to the login page after account-menu logout. Chromium never settles a fetch whose response includes Clear-Site-Data: "cache", so that directive is omitted and the page always navigates even if the logout request rejects.
