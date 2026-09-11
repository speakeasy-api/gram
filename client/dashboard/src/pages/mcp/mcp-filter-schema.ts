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
    | "unproxied";
  /** IDs of the plugins this server is a member of. */
  pluginIds: string[];
  /**
   * The `mcp_servers` id access.listIdentityAccess would name this row by, when
   * one exists.
   *
   * A hosted MCP reaches this listing as its toolset, which has an id of its
   * own, so its server id is resolved through the mcp_servers row pointing back
   * at it. A gateway has no such row at all and therefore cannot be claimed
   * reachable, so it drops out whenever the filter is on.
   */
  serverId?: string;
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

/**
 * The mcp_servers id behind each hosted MCP, keyed by its toolset.
 *
 * Built from the unfiltered mcp_servers list the page already holds: the
 * listing shows hosted MCPs as toolsets, but access is recorded against the
 * mcp_servers row that points at the toolset. `mcpSlug` is NOT that link — it
 * is the server's public MCP slug, which differs from the toolset's own slug
 * and is unset on plenty of rows.
 */
export function serverIdByToolsetId(servers: McpServer[]): Map<string, string> {
  const byToolsetId = new Map<string, string>();
  for (const server of servers) {
    if (server.toolsetId) byToolsetId.set(server.toolsetId, server.id);
  }
  return byToolsetId;
}

export function toolsetFacets(
  toolset: ToolsetEntry,
  membership: PluginMembership,
  serverIds?: ReadonlyMap<string, string>,
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
    serverId: serverIds?.get(toolset.id),
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
  const source = server.unproxiedMcpServerId
    ? "unproxied"
    : server.tunneledMcpServerId
      ? "tunneled"
      : "remote";
  return {
    status,
    source,
    pluginIds: membership.byMcpServerId.get(server.id) ?? [],
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
