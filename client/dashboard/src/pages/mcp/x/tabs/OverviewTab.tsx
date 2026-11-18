import { useRoutes } from "@/routes";
import { useProject } from "@/contexts/Auth";
import { telemetrySearchLogs } from "@gram/client/funcs/telemetrySearchLogs";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { useQuery } from "@tanstack/react-query";
import { serializeFilters } from "@/pages/logs/log-filter-url";
import { Operator } from "@gram/client/models/components/logfilter";
import { Stack } from "@/components/ui/Stack";
import {
  type HostedServerRef,
  MCPOverviewTab,
} from "@/pages/mcp/overview/MCPOverviewTab";
import { standsInForToolset } from "@/pages/mcp/x/MCPServerDetailsRouting";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { TunneledMcpConnectionsPanel } from "./TunneledMcpConnectionsPanel";
import { UnproxiedMcpOverviewTab } from "./UnproxiedMcpOverviewTab";

// Picks the overview for the server's backend. Unproxied servers have no
// Speakeasy-proxied traffic and get their own scoped-down tab; tunneled servers
// get tunnel status and activity on top of the shared usage dashboard.
export function OverviewTab({
  mcpServer,
  agentSetupHref,
  hostedToolset: loadedToolset,
}: {
  mcpServer: McpServer;
  agentSetupHref: string;
  /** The hosted toolset when loaded, for when the read's summary is missing. */
  hostedToolset?: { id: string; slug: string; name: string };
}): JSX.Element | null {
  const routes = useRoutes();
  const client = useGramContext();
  const project = useProject();
  // Tool Logs is empty when the organization does not record logs, so the
  // tunnel view links to it only when it can show something. Tool Logs' own
  // search answers 404 in that case and is open to every member, unlike the
  // organization's feature settings. A one-minute, one-row search keeps the
  // probe cheap.
  const toolLogsProbe = useQuery({
    queryKey: ["tool-logs-available", project.id],
    queryFn: () => {
      const to = new Date();
      return unwrapAsync(
        telemetrySearchLogs(client, {
          searchLogsPayload: {
            from: new Date(to.getTime() - 60_000),
            to,
            limit: 1,
          },
        }),
      );
    },
    enabled: !!mcpServer.tunneledMcpServerId,
    staleTime: 5 * 60_000,
    retry: false,
    throwOnError: false,
  });
  const toolLogsAvailable = toolLogsProbe.isSuccess;
  if (mcpServer.unproxiedMcpServerId) {
    return (
      <UnproxiedMcpOverviewTab
        unproxiedMcpServerId={mcpServer.unproxiedMcpServerId}
      />
    );
  }
  const hosted = standsInForToolset(mcpServer);
  const hostedToolset = hosted
    ? (mcpServer.toolset ?? loadedToolset)
    : undefined;
  // Without the toolset slug a hosted server's traffic can't be shown at all.
  if (hosted && !hostedToolset) return null;
  // Hosted traffic is only attributed by toolset slug, so a hosted server keeps the toolset page's overview.
  const server: HostedServerRef = hostedToolset
    ? {
        kind: "toolset",
        id: hostedToolset.id,
        slug: hostedToolset.slug,
        name: hostedToolset.name,
      }
    : {
        kind: "mcp-server",
        id: mcpServer.id,
        slug: mcpServer.slug ?? "",
        name: mcpServer.name ?? "MCP Server",
      };
  const tunneled = !!mcpServer.tunneledMcpServerId;
  // The usage dashboard is keyed by slug; a tunneled server without one still
  // gets its tunnel status.
  const usage = hostedToolset ? (
    <MCPOverviewTab server={server} />
  ) : mcpServer.slug ? (
    <MCPOverviewTab
      server={server}
      // Tool Logs lists no tunneled servers as filter options yet.
      logsTarget={tunneled ? null : { type: "mcp_server", id: mcpServer.id }}
    />
  ) : null;

  if (!mcpServer.tunneledMcpServerId) return usage;

  const filters = serializeFilters([
    {
      id: "tunnel",
      path: "gram.tunneled_mcp_server.id",
      op: Operator.Eq,
      value: mcpServer.tunneledMcpServerId,
    },
  ]);
  const logsHref = toolLogsAvailable
    ? `${routes.logs.href()}?${new URLSearchParams({ af: filters ?? "" })}`
    : undefined;
  return (
    <Stack gap={6}>
      <TunneledMcpConnectionsPanel
        tunneledMcpServerId={mcpServer.tunneledMcpServerId}
        agentSetupHref={agentSetupHref}
        logsHref={logsHref}
      />
      {usage}
    </Stack>
  );
}
