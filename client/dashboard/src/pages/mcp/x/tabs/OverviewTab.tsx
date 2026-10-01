import { useTelemetry } from "@/contexts/Telemetry";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useRoutes } from "@/routes";
import { serializeFilters } from "@/pages/logs/log-filter-url";
import { Operator } from "@gram/client/models/components/logfilter";
import { PluginStatusBanner } from "@/pages/mcp/overview/PluginStatusBanner";
import { Stack } from "@/components/ui/Stack";
import { MCPOverviewTab } from "@/pages/mcp/overview/MCPOverviewTab";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { TunneledMcpConnectionsPanel } from "./TunneledMcpConnectionsPanel";
import { UnproxiedMcpOverviewTab } from "./UnproxiedMcpOverviewTab";

export function OverviewTab({
  mcpServer,
  agentSetupHref,
}: {
  mcpServer: McpServer;
  agentSetupHref: string;
}): JSX.Element | null {
  const telemetry = useTelemetry();
  const routes = useRoutes();
  const enhanced =
    telemetry.isFeatureEnabled(FEATURE_FLAGS.tunnelObservability) === true;
  if (mcpServer.unproxiedMcpServerId) {
    return (
      <UnproxiedMcpOverviewTab
        unproxiedMcpServerId={mcpServer.unproxiedMcpServerId}
        mcpServerId={mcpServer.id}
        mcpServerSlug={mcpServer.slug ?? ""}
        mcpServerName={mcpServer.name ?? "MCP Server"}
      />
    );
  }
  const server = {
    kind: "mcp-server" as const,
    id: mcpServer.id,
    slug: mcpServer.slug ?? "",
    name: mcpServer.name ?? "MCP Server",
  };
  const usage = mcpServer.slug ? <MCPOverviewTab server={server} /> : null;

  if (!mcpServer.tunneledMcpServerId) return usage;

  const filters = serializeFilters([
    {
      id: "tunnel",
      path: "gram.tunneled_mcp_server.id",
      op: Operator.Eq,
      value: mcpServer.tunneledMcpServerId,
    },
  ]);
  const logsHref = `${routes.logs.href()}?${new URLSearchParams({ af: filters ?? "" })}`;
  return (
    <Stack gap={6}>
      {enhanced && <PluginStatusBanner server={server} />}
      <TunneledMcpConnectionsPanel
        tunneledMcpServerId={mcpServer.tunneledMcpServerId}
        agentSetupHref={agentSetupHref}
        logsHref={logsHref}
      />
      {!enhanced && usage}
    </Stack>
  );
}
