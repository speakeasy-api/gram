import type { PluginSkillDistribution } from "@gram/client/models/components/pluginskilldistribution.js";

/**
 * Pure helpers for the "served over MCP" hints. A Speakeasy-hosted MCP server
 * serves the skills of every plugin that includes it, to the people and
 * agents the plugin is assigned to, so each helper answers "does this plugin
 * (or plugin server) reach a given hosted server?" from the plugin's server
 * list alone.
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
 * Whether a plugin server is one hosted MCP server. A hosted server is
 * referenced by its toolset id, and its canonical mcp_servers row shares that
 * id, so both keys are compared against it.
 */
export function pluginServerServesToolset(
  server: Pick<ServingPluginServer, "toolsetId" | "mcpServerId">,
  toolsetId: string,
): boolean {
  if (!toolsetId) return false;
  return server.toolsetId === toolsetId || server.mcpServerId === toolsetId;
}

/** Whether a plugin includes the hosted MCP server with this toolset id. */
export function pluginServesToolset(
  plugin: Pick<ServingPlugin, "servers">,
  toolsetId: string,
): boolean {
  return (plugin.servers ?? []).some((server) =>
    pluginServerServesToolset(server, toolsetId),
  );
}

/**
 * The plugin servers that serve skills over MCP: the hosted ones. A server
 * carrying a toolset id is hosted; one carrying only an mcp_servers id is
 * hosted when that id names a toolset (its canonical wrapper) and remote
 * otherwise. Pass the known toolset ids to classify the second kind; without
 * them, only toolset-keyed servers count.
 */
export function hostedPluginServers<T extends ServingPluginServer>(
  servers: T[] | undefined,
  toolsetIds?: ReadonlySet<string>,
): T[] {
  return (servers ?? []).filter(
    (server) =>
      !!server.toolsetId ||
      (!!server.mcpServerId && !!toolsetIds?.has(server.mcpServerId)),
  );
}

/** The toolset id a hosted plugin server resolves to, under either key. */
export function hostedPluginServerToolsetId(
  server: Pick<ServingPluginServer, "toolsetId" | "mcpServerId">,
): string | undefined {
  return server.toolsetId ?? server.mcpServerId;
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
