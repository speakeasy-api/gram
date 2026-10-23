import { useBuiltinMcpServers } from "@/components/mcp/useBuiltinMcpServers";
import { useRoutes } from "@/routes";
import { useMemo } from "react";
import type { LauncherCandidate } from "./types";

export function useBuiltinMcpCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const { servers } = useBuiltinMcpServers(enabled);
  const routes = useRoutes();
  return useMemo(
    () =>
      servers
        .filter((server) => !server.hidden)
        .map((server): LauncherCandidate => ({
          id: `builtin-mcp:${server.id}`,
          kind: "mcp_server",
          title: server.name,
          detail: "Built-in MCP server · administrator configuration",
          keywords: ["built-in", "builtin", "mcp", server.key, server.id],
          verbs: ["open"],
          icon: "blocks",
          group: "MCP Servers",
          run: () => routes.mcp.builtin.goTo(server.key),
        })),
    [servers, routes],
  );
}
