---
"server": patch
"dashboard": patch
---

Publish plugins straight from the plugin detail page. After adding or removing a server, or editing a plugin's metadata, a "Publish now" prompt offers a one-click republish — or opens the first-publish dialog for projects not yet connected to GitHub — so there's no need to return to the plugins list to re-publish. The detail page now also shows publish freshness: an "Unpublished changes" badge when the project's current plugin state differs from what was last published, or the last published time when up to date, alongside a durable publish button and a marketplace install banner.

This is backed by new `up_to_date` and `last_published_at` fields on the `plugins.getPublishStatus` API, which compare the project's live plugin fingerprint against the fingerprint last pushed to GitHub. Both fields are absent when the project has no GitHub connection.
