---
"dashboard": minor
---

Add an Organization values card to the Device Agent Setup tab with copy buttons for `org_slug` and `org_token`, so admins can get both without opening a platform walkthrough. Once an agent token exists, the button to mint one now reads "Re-generate token" and creates an additional token instead of rotating: tokens already deployed to devices keep working until they are revoked under Settings → API Keys.
