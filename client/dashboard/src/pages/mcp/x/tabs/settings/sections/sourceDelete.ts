import { formatTunneledMcpDisplay } from "@/lib/sources";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import type { UnproxiedMcpServer } from "@gram/client/models/components/unproxiedmcpserver.js";

// The source row behind an mcp_servers row. Hosted (toolset) and gateway
// servers have none: deleting them deletes only the wrapper.
export type SourceBackedDeleteTarget =
  | { kind: "remote"; source: RemoteMcpServer }
  | { kind: "tunneled"; source: TunneledMcpServer }
  | { kind: "unproxied"; source: UnproxiedMcpServer };

export type SourceDeleteSpec = {
  title: string;
  entityDescription: string;
  confirmLabel: string;
  confirmValue: string;
  successMessage: string;
  failureMessage: string;
};

// Copy and the typed-confirmation value for the cascade dialog. Remote and
// unproxied sources have no slugified name, so the URL is what gets typed;
// a tunneled source has nothing but its name.
export function sourceDeleteSpec(
  target: SourceBackedDeleteTarget,
): SourceDeleteSpec {
  switch (target.kind) {
    case "remote":
      return {
        title: "Delete Remote MCP Server",
        entityDescription: "the remote MCP server",
        confirmLabel: "the server URL",
        confirmValue: target.source.url,
        successMessage: "Remote MCP server deleted",
        failureMessage: "Failed to delete remote MCP server",
      };
    case "tunneled":
      return {
        title: "Delete Tunneled MCP Server",
        entityDescription: "the tunneled MCP source",
        confirmLabel: "the source name",
        confirmValue: formatTunneledMcpDisplay(target.source),
        successMessage: "Tunneled MCP server deleted",
        failureMessage: "Failed to delete tunneled MCP server",
      };
    case "unproxied":
      return {
        title: "Delete Unproxied MCP Server",
        entityDescription: "the unproxied MCP server",
        confirmLabel: "the server URL",
        confirmValue: target.source.url,
        successMessage: "Unproxied MCP server deleted",
        failureMessage: "Failed to delete unproxied MCP server",
      };
  }
}

// The mcpServers.list filter that returns every server backed by the same
// source, or null for servers with no source row.
export function linkedMcpServersFilter(
  mcpServer: Pick<
    McpServer,
    "remoteMcpServerId" | "tunneledMcpServerId" | "unproxiedMcpServerId"
  >,
):
  | { remoteMcpServerId: string }
  | { tunneledMcpServerId: string }
  | { unproxiedMcpServerId: string }
  | null {
  if (mcpServer.remoteMcpServerId) {
    return { remoteMcpServerId: mcpServer.remoteMcpServerId };
  }
  if (mcpServer.tunneledMcpServerId) {
    return { tunneledMcpServerId: mcpServer.tunneledMcpServerId };
  }
  if (mcpServer.unproxiedMcpServerId) {
    return { unproxiedMcpServerId: mcpServer.unproxiedMcpServerId };
  }
  return null;
}

// mcpServers.list is filtered server-side, but applying the same predicate
// client-side guards against a stale or unfiltered cache hit invalidating the
// linkage assumption: the dialog must list exactly what the delete removes.
export function serversBackedBySameSource(
  mcpServer: Pick<
    McpServer,
    "remoteMcpServerId" | "tunneledMcpServerId" | "unproxiedMcpServerId"
  >,
  candidates: McpServer[],
): McpServer[] {
  const filter = linkedMcpServersFilter(mcpServer);
  if (!filter) return [];
  return candidates.filter((candidate) => {
    if ("remoteMcpServerId" in filter) {
      return candidate.remoteMcpServerId === filter.remoteMcpServerId;
    }
    if ("tunneledMcpServerId" in filter) {
      return candidate.tunneledMcpServerId === filter.tunneledMcpServerId;
    }
    return candidate.unproxiedMcpServerId === filter.unproxiedMcpServerId;
  });
}
