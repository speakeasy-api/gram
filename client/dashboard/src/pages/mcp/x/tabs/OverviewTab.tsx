import { useRoutes } from "@/routes";
import { useOrganization } from "@/contexts/Auth";
import { useCanViewToolLogs } from "@/components/observe/observeDeepLink";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { serializeFilters } from "@/pages/logs/log-filter-url";
import { Operator } from "@gram/client/models/components/logfilter";
import { PluginStatusBanner } from "@/pages/mcp/overview/PluginStatusBanner";
import { Stack } from "@/components/ui/Stack";
import { MCPOverviewTab } from "@/pages/mcp/overview/MCPOverviewTab";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { TunneledMcpConnectionsPanel } from "./TunneledMcpConnectionsPanel";
import { UnproxiedMcpOverviewTab } from "./UnproxiedMcpOverviewTab";

// Picks the overview for the server's backend. Unproxied servers have no
// Speakeasy-proxied traffic and get their own scoped-down tab; tunneled servers
// get tunnel status and activity on top of the shared usage dashboard.
export function OverviewTab({
  mcpServer,
  agentSetupHref,
}: {
  mcpServer: McpServer;
  agentSetupHref: string;
}): JSX.Element | null {
  const routes = useRoutes();
  const organization = useOrganization();
  const canViewToolLogs = useCanViewToolLogs();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { enabled: !!mcpServer.tunneledMcpServerId, throwOnError: false },
  );
  // Tool Logs is empty when the organization does not record logs, so the
  // tunnel view links to it only when it can show something.
  const toolLogsAvailable =
    canViewToolLogs && features.data?.logsEnabled === true;
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
  const tunneled = !!mcpServer.tunneledMcpServerId;
  // The usage dashboard is keyed by slug; a tunneled server without one still
  // gets its tunnel status.
  const usage = mcpServer.slug ? (
    <MCPOverviewTab
      server={server}
      // For tunneled servers the marketplace banner sits above the tunnel
      // status, so the usage dashboard must not show it a second time.
      showPluginBanner={!tunneled}
      // Tool Logs cannot currently filter a tunneled target by id. Treating
      // it as hosted or shadow would silently show the wrong server's calls.
      logsTarget={tunneled ? null : { type: "hosted", id: mcpServer.slug }}
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
      <PluginStatusBanner server={server} />
      <TunneledMcpConnectionsPanel
        tunneledMcpServerId={mcpServer.tunneledMcpServerId}
        agentSetupHref={agentSetupHref}
        logsHref={logsHref}
      />
      {usage}
    </Stack>
  );
}
