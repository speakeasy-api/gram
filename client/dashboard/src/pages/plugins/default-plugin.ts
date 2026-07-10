import type { Plugin } from "@gram/client/models/components/plugin.js";

// Whether newly created MCP servers route to this plugin by default. Backed by
// the server's plugins.is_default flag (exposed on the Plugin model). Exactly
// one plugin per project is the default, and it can be moved between plugins
// via setDefaultPlugin.
export function isDefaultPlugin(plugin: Pick<Plugin, "isDefault">): boolean {
  return plugin.isDefault;
}

// Shown in place of "No description" for the default plugin, which is the one
// plugin whose membership isn't purely manual — servers get added to it
// automatically (server/internal/plugins/default_plugin.go's
// AttachToDefaultPlugin, run on toolset MCP-enable / first MCP endpoint).
export const DEFAULT_PLUGIN_DESCRIPTION =
  "Every MCP server you enable is added here automatically, so anyone with the default plugin installed always has access to your latest servers.";
