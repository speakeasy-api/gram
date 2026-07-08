// The management API doesn't expose is_default yet — the slug is a stable
// enough proxy since the server always provisions it as "default".
export function isDefaultPluginSlug(slug: string): boolean {
  return slug === "default";
}

// Shown in place of "No description" for the Default plugin, which is the
// one plugin whose membership isn't purely manual — servers get added to it
// automatically (server/internal/plugins/default_plugin.go's
// AttachToDefaultPlugin, run on toolset MCP-enable / first MCP endpoint).
export const DEFAULT_PLUGIN_DESCRIPTION =
  "Every MCP server you enable is added here automatically, so anyone with the Default plugin installed always has access to your latest servers.";
