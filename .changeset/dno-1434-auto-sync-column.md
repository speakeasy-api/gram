---
"server": patch
---

Internal: adds the nullable `plugins.auto_sync_enabled` column, which will let an admin pause automatic syncing for a single plugin so its changes wait until someone syncs the marketplace by hand. Nothing reads it in this release. `NULL` means the plugin's changes sync automatically, which is how every plugin behaves today. The follow-up wires the setting into the publish paths and the dashboard.
