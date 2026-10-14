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
 * Maps each hosted mcp_servers id to the toolset it fronts. A plugin may
 * include a hosted server under its toolset id or under any mcp_servers row
 * fronting that toolset, and the backend serves skills through every such
 * row, so the dashboard must resolve the row to its toolset the same way.
 */
export type HostedServerIndex = ReadonlyMap<string, string>;

/**
 * Builds the index from the project's MCP server list, where each hosted
 * server names its toolset, plus the toolset ids themselves, since a toolset's
 * canonical wrapper shares its id. Without the server list only canonical
 * wrappers resolve, which is what a reader without MCP read can see.
 */
export function hostedServerIndex(
  mcpServers:
    | ReadonlyArray<{ id: string; toolsetId?: string | undefined }>
    | undefined,
  toolsetIds?: Iterable<string>,
): Map<string, string> {
  const index = new Map<string, string>();
  for (const toolsetId of toolsetIds ?? []) {
    index.set(toolsetId, toolsetId);
  }
  for (const server of mcpServers ?? []) {
    if (server.toolsetId) index.set(server.id, server.toolsetId);
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
  if (server.toolsetId) return server.toolsetId;
  if (!server.mcpServerId) return undefined;
  return index?.get(server.mcpServerId) ?? server.mcpServerId;
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
 * The plugin servers that serve skills over MCP: the hosted ones. A server
 * carrying a toolset id is hosted; one carrying only an mcp_servers id is
 * hosted when the index resolves that id to a toolset, and remote otherwise.
 * Without an index, only toolset-keyed servers count.
 */
export function hostedPluginServers<T extends ServingPluginServer>(
  servers: T[] | undefined,
  index?: HostedServerIndex,
): T[] {
  return (servers ?? []).filter(
    (server) =>
      !!server.toolsetId ||
      (!!server.mcpServerId && !!index?.has(server.mcpServerId)),
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
