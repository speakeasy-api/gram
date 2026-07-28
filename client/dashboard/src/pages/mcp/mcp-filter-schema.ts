import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { Plugin } from "@gram/client/models/components/plugin.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import {
  defineFilters,
  type FilterOption,
  type FilterValues,
  type OptionsById,
} from "@/components/filters";

// No auth facet: hosted list rows lack issuer/OAuth fields, so filtering would hide valid rows.
export const MCP_FILTERS = defineFilters([
  { id: "status", label: "Status", kind: "multiselect" },
  { id: "source", label: "Source", kind: "multiselect" },
  { id: "plugins", label: "Included in plugins", kind: "multiselect" },
]);

export const MCP_FILTER_OPTIONS: OptionsById = {
  status: [
    { value: "public", label: "Public" },
    { value: "private", label: "Private" },
    { value: "disabled", label: "Disabled" },
  ],
  source: [
    { value: "catalog", label: "Catalog" },
    { value: "custom", label: "Custom" },
    { value: "remote", label: "Remote URL" },
    { value: "tunneled", label: "Tunneled" },
  ],
  // `plugins` is org data, so its options are supplied at render time via
  // pluginFilterOptions().
};

export interface McpFacets {
  status: "public" | "private" | "disabled";
  source: "catalog" | "custom" | "remote" | "tunneled";
  /** IDs of the plugins this server is a member of. */
  pluginIds: string[];
}

/**
 * Plugin membership is stored on the plugin (`plugin.servers[]`), not on the
 * server, and a plugin server is backed by *either* a toolset (Hosted MCP) or
 * an mcp_server row (Remote/Tunneled). Invert that into per-collection lookups
 * so each listing row can resolve its plugins in O(1).
 */
export interface PluginMembership {
  byToolsetId: Map<string, string[]>;
  byMcpServerId: Map<string, string[]>;
}

export function pluginMembership(plugins: Plugin[]): PluginMembership {
  const byToolsetId = new Map<string, string[]>();
  const byMcpServerId = new Map<string, string[]>();

  const push = (map: Map<string, string[]>, key: string, pluginId: string) => {
    const existing = map.get(key);
    if (existing) existing.push(pluginId);
    else map.set(key, [pluginId]);
  };

  for (const plugin of plugins) {
    for (const server of plugin.servers ?? []) {
      if (server.toolsetId) push(byToolsetId, server.toolsetId, plugin.id);
      if (server.mcpServerId)
        push(byMcpServerId, server.mcpServerId, plugin.id);
    }
  }

  return { byToolsetId, byMcpServerId };
}

export function pluginFilterOptions(plugins: Plugin[]): FilterOption[] {
  return plugins.map((plugin) => ({ value: plugin.id, label: plugin.name }));
}

export function toolsetFacets(
  toolset: ToolsetEntry,
  membership: PluginMembership,
): McpFacets {
  const status = !toolset.mcpEnabled
    ? "disabled"
    : toolset.mcpIsPublic
      ? "public"
      : "private";
  // registrySpecifier is the only list-row signal that a hosted MCP came from catalog.
  const source = toolset.origin?.registrySpecifier ? "catalog" : "custom";
  return {
    status,
    source,
    pluginIds: membership.byToolsetId.get(toolset.id) ?? [],
  };
}

export function mcpServerFacets(
  server: McpServer,
  membership: PluginMembership,
): McpFacets {
  const status =
    server.visibility === "public"
      ? "public"
      : server.visibility === "private"
        ? "private"
        : "disabled";
  return {
    status,
    source: server.tunneledMcpServerId ? "tunneled" : "remote",
    pluginIds: membership.byMcpServerId.get(server.id) ?? [],
  };
}

export function matchesMcpFilters(
  facets: McpFacets,
  values: FilterValues<typeof MCP_FILTERS>,
): boolean {
  return (
    (values.status.length === 0 || values.status.includes(facets.status)) &&
    (values.source.length === 0 || values.source.includes(facets.source)) &&
    // A server matches if it belongs to *any* of the selected plugins.
    (values.plugins.length === 0 ||
      facets.pluginIds.some((id) => values.plugins.includes(id)))
  );
}

export function hasActiveMcpFilters(
  values: FilterValues<typeof MCP_FILTERS>,
): boolean {
  return (
    values.status.length > 0 ||
    values.source.length > 0 ||
    values.plugins.length > 0
  );
}
