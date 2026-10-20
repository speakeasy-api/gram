import type { PluginSkillDistribution } from "@gram/client/models/components/pluginskilldistribution.js";

/**
 * Pure helpers for the "served over MCP" hints. A Speakeasy-hosted MCP server
 * serves the skills of every plugin that includes it, to the people and
 * agents the plugin is assigned to, so each helper answers "does this plugin
 * (or plugin server) reach a given hosted server?" from the plugin's server
 * list plus an index of the project's hosted servers.
 */

/** The server fields a plugin server needs for the membership check. */
export type ServingPluginServer = {
  id: string;
  displayName: string;
  toolsetId?: string | undefined;
  mcpServerId?: string | undefined;
};

/** A plugin with the servers it includes, as listPlugins returns them. */
export type ServingPlugin = {
  id: string;
  name: string;
  servers?: ServingPluginServer[] | undefined;
};

/**
 * Maps each mcp_servers id the caller can see to the toolset it fronts. A
 * plugin may include a hosted server under its toolset id or under any
 * mcp_servers row fronting that toolset, and the backend serves skills
 * through every such row, so the dashboard must resolve the row to its
 * toolset the same way. A listed id that serves no skills maps to null: a
 * remote server, or a toolset's canonical wrapper while the toolset has MCP
 * turned off.
 */
export type HostedServerIndex = ReadonlyMap<string, string | null>;

/** A toolset as the index needs it; MCP counts as on unless it is false. */
export type IndexedToolset = { id: string; mcpEnabled?: boolean | undefined };

/**
 * Builds the index from the project's MCP server list, where each hosted
 * server names its toolset, plus the toolsets themselves, since a toolset's
 * canonical wrapper shares its id. Without the server list only canonical
 * wrappers resolve, which is what a reader without MCP read can see.
 */
export function hostedServerIndex(
  mcpServers:
    | ReadonlyArray<{ id: string; toolsetId?: string | undefined }>
    | undefined,
  toolsets?: Iterable<IndexedToolset>,
): Map<string, string | null> {
  const index = new Map<string, string | null>();
  const mcpOff = new Set<string>();
  for (const toolset of toolsets ?? []) {
    if (toolset.mcpEnabled === false) mcpOff.add(toolset.id);
    index.set(toolset.id, toolset.id);
  }
  for (const server of mcpServers ?? []) {
    index.set(server.id, server.toolsetId ?? null);
  }
  // The canonical endpoint answers only while the toolset has MCP on; other
  // wrappers of the toolset keep serving, so the toolset stays reachable
  // while one of them exists.
  for (const toolsetId of mcpOff) {
    const hasOtherWrapper = [...index].some(
      ([id, target]) => target === toolsetId && id !== toolsetId,
    );
    if (!hasOtherWrapper) index.set(toolsetId, null);
  }
  return index;
}

/**
 * The digest an agent sees for a skill version over MCP: the SHA-256 of the
 * exact SKILL.md bytes, in the `sha256:<hex>` form the Skills extension uses.
 */
export function servedSkillDigest(rawSha256: string): string {
  return `sha256:${rawSha256.toLowerCase()}`;
}

/** A readable prefix of the digest for labels; the full value goes in a tooltip. */
export function shortServedSkillDigest(rawSha256: string): string {
  return `sha256:${rawSha256.slice(0, 12).toLowerCase()}…`;
}

/**
 * The toolset id a plugin server resolves to, under either key. A server
 * keyed by an mcp_servers id resolves through the index; an id the index
 * does not know is taken as a canonical wrapper, whose id is the toolset's,
 * so a reader who cannot list servers still sees canonical membership.
 */
export function hostedPluginServerToolsetId(
  server: Pick<ServingPluginServer, "toolsetId" | "mcpServerId">,
  index?: HostedServerIndex,
): string | undefined {
  if (server.toolsetId) {
    return index?.get(server.toolsetId) === null ? undefined : server.toolsetId;
  }
  if (!server.mcpServerId) return undefined;
  if (index?.has(server.mcpServerId)) {
    return index.get(server.mcpServerId) ?? undefined;
  }
  return server.mcpServerId;
}

/** Whether a plugin server is the hosted MCP server with this toolset id. */
export function pluginServerServesToolset(
  server: Pick<ServingPluginServer, "toolsetId" | "mcpServerId">,
  toolsetId: string,
  index?: HostedServerIndex,
): boolean {
  if (!toolsetId) return false;
  return hostedPluginServerToolsetId(server, index) === toolsetId;
}

/** Whether a plugin includes the hosted MCP server with this toolset id. */
export function pluginServesToolset(
  plugin: Pick<ServingPlugin, "servers">,
  toolsetId: string,
  index?: HostedServerIndex,
): boolean {
  return (plugin.servers ?? []).some((server) =>
    pluginServerServesToolset(server, toolsetId, index),
  );
}

/**
 * Whether a plugin includes the mcp_servers row with this id. A remote server
 * has no toolset, so a plugin includes it only under its own row id.
 */
export function pluginServesMcpServer(
  plugin: Pick<ServingPlugin, "servers">,
  mcpServerId: string,
): boolean {
  if (!mcpServerId) return false;
  return (plugin.servers ?? []).some(
    (server) => server.mcpServerId === mcpServerId,
  );
}

/**
 * The plugin servers that serve skills over MCP, split by what the index can
 * prove. A server carrying a toolset id is hosted unless the index shows that
 * toolset with MCP off and no other wrapper. One carrying only an mcp_servers
 * id is hosted when the index resolves that id to a toolset, and dropped when
 * the index lists it as serving nothing (a remote server, say); an id the
 * index does not know is unresolved, not remote: the server list is filtered
 * by the caller's grants, so the row may be a hosted wrapper the caller
 * cannot view. Without an index every such server is unresolved.
 */
export type HostedPluginServers<T> = {
  /** Servers known to be hosted, in plugin order. */
  servers: T[];
  /** Servers keyed by an mcp_servers id the caller cannot resolve. */
  unresolved: T[];
};

export function hostedPluginServers<T extends ServingPluginServer>(
  servers: T[] | undefined,
  index?: HostedServerIndex,
): HostedPluginServers<T> {
  const result: HostedPluginServers<T> = { servers: [], unresolved: [] };
  for (const server of servers ?? []) {
    if (server.toolsetId) {
      if (index?.get(server.toolsetId) !== null) result.servers.push(server);
    } else if (server.mcpServerId) {
      if (!index?.has(server.mcpServerId)) result.unresolved.push(server);
      else if (index.get(server.mcpServerId)) result.servers.push(server);
    }
  }
  return result;
}

/** Whether any of the plugins includes a server the caller cannot resolve. */
export function pluginsHaveUnresolvedServers(
  plugins: ReadonlyArray<Pick<ServingPlugin, "servers">>,
  index?: HostedServerIndex,
): boolean {
  return plugins.some(
    (plugin) =>
      hostedPluginServers(plugin.servers, index).unresolved.length > 0,
  );
}

export type ServedSkillGroup<
  T extends { pluginId: string },
  P extends ServingPlugin = ServingPlugin,
> = {
  key: string;
  plugin: P;
  items: T[];
};

/**
 * Groups distributions under the plugins that include the hosted server, in
 * plugin order, dropping distributions of any other plugin. Plugins with no
 * distributions are kept so the page can name where to add the first skill.
 */
export function groupServedSkillsByPlugin<
  T extends Pick<PluginSkillDistribution, "pluginId">,
  P extends ServingPlugin,
>(distributions: T[], servingPlugins: P[]): ServedSkillGroup<T, P>[] {
  return servingPlugins.map((plugin) => ({
    key: plugin.id,
    plugin,
    items: distributions.filter(
      (distribution) => distribution.pluginId === plugin.id,
    ),
  }));
}
