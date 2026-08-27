import type { MCPServerEntry } from "@/elements";

type ToolsetUrlSource = {
  slug: string;
  mcpSlug?: string;
  defaultEnvironmentSlug?: string;
};

type McpServerUrlSource = {
  id: string;
  slug?: string;
  unproxiedMcpServerId?: string;
  visibility: string;
};

type EndpointUrlSource = {
  slug: string;
  mcpServerId?: string;
  customDomainId?: string;
};

/**
 * Client MCP entries for the insights dock tools/list. Undefined while any
 * listing is still in flight or unknown so the picker stays on loading.
 * Settled empty is `[]`.
 */
export function observabilityMcpEntries({
  projectSlug,
  serverURL,
  toolsetsLoading,
  toolsets,
  mcpServersLoading,
  mcpServers,
  endpointsLoading,
  endpoints,
}: {
  projectSlug: string;
  serverURL: string;
  toolsetsLoading: boolean;
  toolsets: readonly ToolsetUrlSource[] | undefined;
  mcpServersLoading: boolean;
  mcpServers: readonly McpServerUrlSource[] | undefined;
  endpointsLoading: boolean;
  endpoints: readonly EndpointUrlSource[] | undefined;
}): MCPServerEntry[] | undefined {
  if (toolsetsLoading || mcpServersLoading || endpointsLoading) {
    return undefined;
  }
  if (
    toolsets === undefined ||
    mcpServers === undefined ||
    endpoints === undefined
  ) {
    return undefined;
  }

  const seen = new Set<string>();
  const entries: MCPServerEntry[] = [];

  for (const toolset of toolsets) {
    const url = toolsetMcpUrl(serverURL, projectSlug, toolset);
    if (!url || seen.has(url)) continue;
    seen.add(url);
    entries.push({
      url,
      name: toolset.slug,
      environment: toolset.defaultEnvironmentSlug,
    });
  }

  const serverById = new Map(mcpServers.map((server) => [server.id, server]));
  const endpointByServer = new Map<string, EndpointUrlSource>();
  for (const endpoint of endpoints) {
    if (!endpoint.mcpServerId) continue;
    const existing = endpointByServer.get(endpoint.mcpServerId);
    if (!existing || (existing.customDomainId && !endpoint.customDomainId)) {
      endpointByServer.set(endpoint.mcpServerId, endpoint);
    }
  }

  for (const [serverId, endpoint] of endpointByServer) {
    const server = serverById.get(serverId);
    if (
      !server ||
      server.visibility === "disabled" ||
      server.unproxiedMcpServerId
    ) {
      continue;
    }
    const url = `${serverURL}/mcp/${endpoint.slug}`;
    if (seen.has(url)) continue;
    seen.add(url);
    entries.push({
      url,
      name: server.slug ?? endpoint.slug,
    });
  }

  return entries;
}

/** Same suffix rules as `internalMcpUrl`, with a caller-supplied origin. */
function toolsetMcpUrl(
  serverURL: string,
  projectSlug: string,
  toolset: ToolsetUrlSource,
): string | undefined {
  const suffix = toolset.mcpSlug
    ? toolset.mcpSlug
    : toolset.defaultEnvironmentSlug
      ? [projectSlug, toolset.slug, toolset.defaultEnvironmentSlug].join("/")
      : undefined;
  return suffix ? `${serverURL}/mcp/${suffix}` : undefined;
}
