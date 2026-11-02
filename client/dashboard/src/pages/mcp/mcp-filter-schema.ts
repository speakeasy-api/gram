import type { Plugin } from "@gram/client/models/components/plugin.js";
import type { ListedMcpServer } from "@/components/mcp/mcp-server-kind";
import { standsInForToolset } from "@/pages/mcp/x/MCPServerDetailsRouting";
import {
  defineFilters,
  type FilterOption,
  type FilterValues,
  type OptionsById,
} from "@/components/filters";

// No auth facet: hosted list rows lack issuer/OAuth fields, so filtering would hide valid rows.
export const MCP_FILTERS = defineFilters([
  { id: "status", label: "Status", kind: "multiselect" },
  {
    id: "source",
    label: "Source",
    kind: "multiselect",
    description: "Where the server came from and how Speakeasy reaches it.",
  },
  { id: "plugins", label: "Included in plugins", kind: "multiselect" },
  {
    id: "accessibleBy",
    label: "Accessible by",
    kind: "multiselect",
    description:
      "Who is authorized to reach the server, through a grant on them or on a role they hold. Plugin membership is distribution, not access, so it does not widen this.",
  },
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
    { value: "gateway", label: "Gateway" },
    { value: "remote", label: "Remote URL" },
    { value: "tunneled", label: "Tunneled" },
    { value: "unproxied", label: "Unproxied" },
    { value: "built_in", label: "Built-in" },
  ],
  // `plugins` is org data, so its options are supplied at render time via
  // pluginFilterOptions().
};

export interface McpFacets {
  /** Absent for gateways, which have no visibility; an active status filter excludes them. */
  status?: "public" | "private" | "disabled";
  source:
    | "catalog"
    | "custom"
    | "gateway"
    | "remote"
    | "tunneled"
    | "unproxied"
    | "built_in";
  /** IDs of the plugins this server is a member of. */
  pluginIds: string[];
  /** The mcp_servers id access is recorded against; gateways have none. */
  serverId?: string;
}

/** Plugin entries keyed by mcp_servers id or, for older hosted ones, toolset id, inverted for O(1) lookup. */
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

export function mcpServerFacets(
  { server, kind }: ListedMcpServer,
  membership: PluginMembership,
): McpFacets {
  const status =
    server.visibility === "public"
      ? "public"
      : server.visibility === "private"
        ? "private"
        : "disabled";
  // registrySpecifier is the only list-row signal that a hosted MCP came from catalog.
  const source =
    kind === "hosted"
      ? server.toolset?.registrySpecifier
        ? "catalog"
        : "custom"
      : kind;
  // Older plugin entries name the toolset itself, which its hosting server stands for.
  const pluginIds = new Set(membership.byMcpServerId.get(server.id) ?? []);
  if (kind === "hosted" && server.toolsetId && standsInForToolset(server)) {
    for (const id of membership.byToolsetId.get(server.toolsetId) ?? [])
      pluginIds.add(id);
  }
  return {
    status,
    source,
    pluginIds: [...pluginIds],
    serverId: server.id,
  };
}

export function gatewayFacets(): McpFacets {
  return { source: "gateway", pluginIds: [] };
}

export function matchesMcpFilters(
  facets: McpFacets,
  values: FilterValues<typeof MCP_FILTERS>,
  /**
   * Every server id reachable by the selected people, unioned. Supplied by the
   * page because it is fetched per selected user; `undefined` while those reads
   * are still in flight, which matches nothing rather than showing rows we
   * cannot yet say are reachable.
   */
  reachableServerIds?: ReadonlySet<string>,
): boolean {
  return (
    (values.status.length === 0 ||
      (facets.status !== undefined && values.status.includes(facets.status))) &&
    (values.source.length === 0 || values.source.includes(facets.source)) &&
    // A server matches if it belongs to *any* of the selected plugins.
    (values.plugins.length === 0 ||
      facets.pluginIds.some((id) => values.plugins.includes(id))) &&
    // Likewise reachable by *any* of the selected people.
    (values.accessibleBy.length === 0 ||
      (reachableServerIds !== undefined &&
        facets.serverId !== undefined &&
        reachableServerIds.has(facets.serverId)))
  );
}

export function hasActiveMcpFilters(
  values: FilterValues<typeof MCP_FILTERS>,
): boolean {
  return (
    values.status.length > 0 ||
    values.source.length > 0 ||
    values.plugins.length > 0 ||
    values.accessibleBy.length > 0
  );
}
