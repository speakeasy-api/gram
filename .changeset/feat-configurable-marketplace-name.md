---
"server": minor
"@gram/client": minor
"dashboard": minor
---

Make the generated marketplace name configurable per-project. Adds `plugins.getMarketplaceSettings` and `plugins.updateMarketplaceSettings` on the management API plus a Marketplace settings dialog in the Plugins tab. The default is now `<org-slug>-speakeasy` (previously `<org-slug>-gram`); the org-slug prefix keeps defaults unique across customers so end users installing from two Gram marketplaces don't collide. Saving an override on a project that already has a published marketplace auto-republishes the new manifest to GitHub. References to "Gram" in the generated README, plugin descriptions, and hook scripts are rebranded to "Speakeasy"; URLs, env-var names, and HTTP header names are unchanged.
