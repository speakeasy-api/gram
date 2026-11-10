import type { ExternalMcpOAuthConfigStatus } from "@/components/sources/sources-hooks";
import { mcpServerRouteParam } from "@/lib/sources";
import {
  isCanonicalHostedWrapper,
  standsInForToolset,
} from "@/pages/mcp/x/MCPServerDetailsRouting";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { McpServerToolsetSummary } from "@gram/client/models/components/mcpservertoolsetsummary.js";
import type { useRoutes } from "@/routes";

export type McpServerKind = "hosted" | "remote" | "tunneled" | "unproxied";

export const MCP_SERVER_KIND_LABEL: Record<McpServerKind, string> = {
  hosted: "Hosted",
  remote: "Remote",
  tunneled: "Tunneled",
  unproxied: "Unproxied",
};

export interface ListedMcpServer {
  server: McpServer;
  kind: McpServerKind;
}

/** How Speakeasy reaches the server; undefined for a row with no backend. */
function mcpServerKind(server: McpServer): McpServerKind | undefined {
  if (server.unproxiedMcpServerId) return "unproxied";
  if (server.tunneledMcpServerId) return "tunneled";
  if (server.remoteMcpServerId) return "remote";
  if (server.toolsetId) return "hosted";
  return undefined;
}

/**
 * Every server with a backend. A hosted wrapper that doesn't host its toolset
 * is hidden when it has no endpoint: it is a gateway's member wrapper and
 * shows under that gateway. When has_endpoint is unknown it is listed.
 */
export function listedMcpServers(servers: McpServer[]): ListedMcpServer[] {
  const listed: ListedMcpServer[] = [];
  for (const server of servers) {
    const kind = mcpServerKind(server);
    if (kind === undefined) continue;
    if (
      kind === "hosted" &&
      !standsInForToolset(server) &&
      server.hasEndpoint === false
    ) {
      continue;
    }
    listed.push({ server, kind });
  }
  return listed;
}

/** The toolset summary when this row hosts its toolset. */
function standInToolset(
  server: McpServer,
): McpServerToolsetSummary | undefined {
  return mcpServerKind(server) === "hosted" && standsInForToolset(server)
    ? server.toolset
    : undefined;
}

export function mcpServerDisplayName(server: McpServer): string {
  // A canonical server's name is its toolset's; any other server is renamed on its own row.
  const preferred = isCanonicalHostedWrapper(server)
    ? server.toolset?.name || server.name
    : server.name || server.toolset?.name;
  return preferred || "MCP Server";
}

/** Undefined when the toolset summary is missing, so no count can be claimed. */
export function hostedToolCountLabel(
  toolset: McpServerToolsetSummary | undefined,
): string | undefined {
  if (!toolset) return undefined;
  // A proxy can't list its tools until someone signs in, so no count is true.
  if (toolset.externalMcpProxyOnly) return "Tools listed once you sign in";
  return `${toolset.toolCount} ${toolset.toolCount === 1 ? "tool" : "tools"}`;
}

/** Every server, hosted ones included, is managed on its own server page. */
export function mcpServerHref(
  routes: ReturnType<typeof useRoutes>,
  server: McpServer,
  oauthStatus: ExternalMcpOAuthConfigStatus,
): string {
  const param = mcpServerRouteParam(server);
  if (oauthStatus !== "required-unconfigured") {
    return routes.mcp.details.overview.href(param);
  }
  // A hosted server's sign-in setup is a section of its settings.
  return standInToolset(server)
    ? `${routes.mcp.details.settings.href(param)}#authentication`
    : routes.mcp.details.settings.href(param);
}
