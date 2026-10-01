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

// Picks the overview for the server's backend. Unproxied servers have no
// Speakeasy-proxied traffic and get their own scoped-down tab; tunneled servers
// get the live connections panel on top of the shared usage dashboard.
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
  // The usage dashboard is keyed by slug; a tunneled server without one still
  // gets its connections panel.
  const usage = mcpServer.slug ? (
    <MCPOverviewTab
      server={server}
      // Tool Logs cannot currently filter a tunneled target by id. Treating
      // it as hosted or shadow would silently show the wrong server's calls.
      logsTarget={
        mcpServer.tunneledMcpServerId
          ? null
          : { type: "hosted", id: mcpServer.slug }
      }
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
