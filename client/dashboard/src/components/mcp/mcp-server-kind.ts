import type { ExternalMcpOAuthConfigStatus } from "@/components/sources/sources-hooks";
import { mcpServerRouteParam } from "@/lib/sources";
import { isCanonicalHostedWrapper } from "@/pages/mcp/x/MCPServerDetailsRouting";
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

/** Every server with a backend, each listed once under its own row. */
export function listedMcpServers(servers: McpServer[]): ListedMcpServer[] {
  const listed: ListedMcpServer[] = [];
  for (const server of servers) {
    const kind = mcpServerKind(server);
    if (kind !== undefined) listed.push({ server, kind });
  }
  return listed;
}

/** The toolset summary when this row stands in for its toolset (the canonical wrapper). */
export function canonicalToolset(
  server: McpServer,
): McpServerToolsetSummary | undefined {
  return mcpServerKind(server) === "hosted" && isCanonicalHostedWrapper(server)
    ? server.toolset
    : undefined;
}

export function mcpServerDisplayName(server: McpServer): string {
  const preferred = canonicalToolset(server)
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

/** A canonical wrapper is managed on its toolset page; every other row on its own server page. */
export function mcpServerHref(
  routes: ReturnType<typeof useRoutes>,
  server: McpServer,
  oauthStatus: ExternalMcpOAuthConfigStatus,
): string {
  const toolsetSlug = canonicalToolset(server)?.slug;
  if (!toolsetSlug) {
    return routes.mcp.x.overview.href(mcpServerRouteParam(server));
  }
  return oauthStatus === "required-unconfigured"
    ? routes.mcp.details.authentication.href(toolsetSlug)
    : routes.mcp.details.href(toolsetSlug);
}
