---
"dashboard": minor
"server": minor
---

Let admins choose which plugin newly created MCP servers route to by default. The default was previously fixed to the auto-provisioned "Default" plugin; now the `plugins.is_default` flag is exposed on the plugin model and a new `setDefaultPlugin` endpoint (surfaced as a "Set as default" action on the plugin page) moves the default to any plugin in the project.
