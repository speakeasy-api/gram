import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { PluginServer } from "@gram/client/models/components/pluginserver.js";
import { isCanonicalHostedWrapper } from "@/pages/mcp/x/MCPServerDetailsRouting";
import type { HostedServerRef } from "./MCPOverviewTab";

/**
 * How plugin entries name a server: by its toolset when it is the toolset's
 * own wrapper, and by its own id otherwise. The read can omit the toolset
 * summary, so the caller may pass the loaded toolset.
 */
export function pluginServerRef(
  mcpServer: McpServer,
  toolset?: { id: string; slug: string; name: string },
): HostedServerRef {
  const toolsetRef = mcpServer.toolset ?? toolset;
  return isCanonicalHostedWrapper(mcpServer) && toolsetRef
    ? {
        kind: "toolset",
        id: toolsetRef.id,
        slug: toolsetRef.slug,
        name: toolsetRef.name,
      }
    : {
        kind: "mcp-server",
        id: mcpServer.id,
        slug: mcpServer.slug ?? "",
        name: mcpServer.name ?? "MCP Server",
      };
}

/** Whether a plugin entry is this server, by toolset or by MCP server id. */
export function serverMatchesRef(
  server: PluginServer,
  ref: HostedServerRef,
): boolean {
  // A hosted entry may be keyed by its canonical mcp_server, which shares the toolset id.
  return ref.kind === "toolset"
    ? server.toolsetId === ref.id || server.mcpServerId === ref.id
    : server.mcpServerId === ref.id;
}
