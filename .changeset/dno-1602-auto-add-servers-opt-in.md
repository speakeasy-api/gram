---
"dashboard": minor
"server": minor
---

Automatic MCP server adding for plugins is now opt-in. Each plugin has an Add MCP servers automatically switch on its Settings tab, and it is off by default, including for existing plugins and plugins that role setup creates. While it is off, assigning a role to the plugin, changing a role's access, and new servers becoming available never add or remove the plugin's MCP servers; only people who edit the plugin change its server list. Turning it on adds the servers the plugin's roles can use now, except ones an admin removed before, and keeps the list in step with later access changes. Turning it off keeps the current servers. The Platform MCP plugin list reports the setting. When creating a plugin in the dashboard, both Sync changes automatically and Add MCP servers automatically now sit under Advanced settings and start off.
