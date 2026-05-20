---
"server": minor
"@gram/client": minor
"dashboard": minor
---

Make the generated marketplace name configurable per-project. Adds `plugins.getMarketplaceSettings` and `plugins.updateMarketplaceSettings` on the management API plus a new Marketplace settings section in the Plugins tab. The default is now `speakeasy` (previously `<org-slug>-gram`), and saving an override on a project that already has a published marketplace auto-republishes the new manifest to GitHub. References to "Gram" in the generated README, plugin descriptions, and hook scripts are rebranded to "Speakeasy"; URLs, env-var names, and HTTP header names are unchanged.
