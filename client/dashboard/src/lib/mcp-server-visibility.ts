import type {
  McpServer,
  McpServerVisibility,
} from "@gram/client/models/components/mcpserver.js";
import type { UpdateMcpServerForm } from "@gram/client/models/components/updatemcpserverform.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import type { QueryClient } from "@tanstack/react-query";

/**
 * Builds the full `UpdateMcpServer` request body for a visibility change.
 * The update endpoint replaces the whole record, so every existing field is
 * carried over and only `visibility` changes.
 */
export function mcpServerVisibilityUpdateForm(
  mcpServer: McpServer,
  visibility: McpServerVisibility,
): UpdateMcpServerForm {
  return {
    id: mcpServer.id,
    name: mcpServer.name ?? undefined,
    remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
    tunneledMcpServerId: mcpServer.tunneledMcpServerId ?? undefined,
    toolsetId: mcpServer.toolsetId ?? undefined,
    unproxiedMcpServerId: mcpServer.unproxiedMcpServerId ?? undefined,
    environmentId: mcpServer.environmentId ?? undefined,
    toolVariationsGroupId: mcpServer.toolVariationsGroupId ?? undefined,
    visibility,
  };
}

export function mcpServerVisibilityToast(
  visibility: McpServerVisibility,
): string {
  switch (visibility) {
    case "disabled":
      return "MCP server disabled";
    case "private":
      return "MCP server enabled";
    case "public":
      return "MCP server set to public";
    default:
      return "MCP server updated";
  }
}

/**
 * Refreshes every query that reflects an MCP server's visibility: the server
 * list, the server detail, and the endpoints that serve (or stop serving)
 * traffic for it.
 */
export async function invalidateMcpServerQueries(
  queryClient: QueryClient,
): Promise<void> {
  await Promise.all([
    invalidateAllMcpServers(queryClient, { refetchType: "all" }),
    invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
    invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
  ]);
}
