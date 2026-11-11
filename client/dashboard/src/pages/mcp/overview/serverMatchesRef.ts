import type { PluginServer } from "@gram/client/models/components/pluginserver.js";
import type { HostedServerRef } from "./MCPOverviewTab";

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
