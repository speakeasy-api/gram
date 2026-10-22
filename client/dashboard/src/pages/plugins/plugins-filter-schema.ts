import type { Plugin } from "@gram/client/models/components/plugin.js";
import type { PluginServer } from "@gram/client/models/components/pluginserver.js";
import {
  defineFilters,
  type FilterValues,
  type OptionsById,
} from "@/components/filters";

/**
 * Single dimension: which underlying MCP servers a plugin includes. Keyed by
 * backend resource (`mcpServer:<id>`) rather than the plugin_server row id,
 * since the same server can be attached to multiple plugins under different
 * row ids.
 */
export const PLUGINS_FILTERS = defineFilters([
  {
    id: "servers",
    label: "Including servers",
    kind: "multiselect",
    allLabel: "All servers",
  },
]);

// A hosted entry keyed by its toolset and one keyed by its canonical server share an id.
function serverKey(server: PluginServer): string {
  return `mcpServer:${server.toolsetId ?? server.mcpServerId}`;
}

/** Distinct servers across all plugins, for the "Including servers" options list. */
export function pluginServerFilterOptions(plugins: Plugin[]): OptionsById {
  const seen = new Map<string, string>();
  for (const plugin of plugins) {
    for (const server of plugin.servers ?? []) {
      const key = serverKey(server);
      if (!seen.has(key)) seen.set(key, server.displayName);
    }
  }
  return {
    servers: Array.from(seen, ([value, label]) => ({ value, label })).sort(
      (a, b) => a.label.localeCompare(b.label),
    ),
  };
}

/** OR within the dimension: a plugin matches if it includes any selected server. */
export function matchesPluginFilters(
  plugin: Plugin,
  values: FilterValues<typeof PLUGINS_FILTERS>,
): boolean {
  if (values.servers.length === 0) return true;
  const keys = new Set((plugin.servers ?? []).map(serverKey));
  // Links saved before hosted entries moved to the server key say `toolset:<id>`.
  return values.servers.some((v) =>
    keys.has(v.replace(/^toolset:/, "mcpServer:")),
  );
}
