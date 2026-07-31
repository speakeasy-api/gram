import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { McpServerToolsetSummary } from "@gram/client/models/components/mcpservertoolsetsummary.js";

export function mcpServerDisplayName(server: McpServer): string {
  return server.name?.trim() || server.toolsetSummary?.name || "MCP Server";
}

// Tool URNs take the form `tools:<kind>:<source>:<name>`. External MCP
// "proxy" entries (`tools:externalmcp:<slug>:proxy`) represent servers that
// can't enumerate their tools until a user authenticates against them.
function isExternalMcpProxyUrn(urn: string): boolean {
  return urn.startsWith("tools:externalmcp:") && urn.endsWith(":proxy");
}

export function hasExternalMcpProxy(
  summary: McpServerToolsetSummary | undefined,
): boolean {
  return !!summary?.toolUrns.some(isExternalMcpProxyUrn);
}

// The tool names shown in listing badges: the last URN segment of every
// non-proxy tool.
export function visibleToolNames(
  summary: McpServerToolsetSummary | undefined,
): string[] {
  return (summary?.toolUrns ?? [])
    .filter((urn) => !isExternalMcpProxyUrn(urn))
    .map((urn) => urn.split(":").pop() || urn);
}

// The external MCP source slug referenced by a toolset summary, used to
// resolve catalog branding (logo) for the listing row.
export function externalMcpSlug(
  summary: McpServerToolsetSummary | undefined,
): string | undefined {
  const urn = summary?.toolUrns.find((u) => u.includes(":externalmcp:"));
  return urn?.split(":")[2];
}
