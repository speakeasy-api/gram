import { invalidateAllGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { invalidateAllGetTunneledMcpServer } from "@gram/client/react-query/getTunneledMcpServer.js";
import { invalidateAllRemoteMcpServers } from "@gram/client/react-query/remoteMcpServers.js";
import { invalidateAllTunneledMcpServers } from "@gram/client/react-query/tunneledMcpServers.js";
import type { QueryClient } from "@tanstack/react-query";

// A source edit has two consumers: the per-id query the settings sections and
// sidebar read from, and the project-wide list the sources shelf reads from.
// refetchType "all" refreshes the list even while nothing observes it.
export async function invalidateTunneledMcpSourceViews(
  queryClient: QueryClient,
): Promise<void> {
  await Promise.all([
    invalidateAllGetTunneledMcpServer(queryClient, { refetchType: "all" }),
    invalidateAllTunneledMcpServers(queryClient, { refetchType: "all" }),
  ]);
}

export async function invalidateRemoteMcpSourceViews(
  queryClient: QueryClient,
): Promise<void> {
  await Promise.all([
    invalidateAllGetRemoteMcpServer(queryClient, { refetchType: "all" }),
    invalidateAllRemoteMcpServers(queryClient, { refetchType: "all" }),
  ]);
}
