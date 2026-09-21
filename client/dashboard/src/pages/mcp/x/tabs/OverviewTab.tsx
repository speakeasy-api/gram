import { Stack } from "@/components/ui/Stack";
import { MCPOverviewTab } from "@/pages/mcp/overview/MCPOverviewTab";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { TunneledMcpConnectionsPanel } from "./TunneledMcpConnectionsPanel";
import { UnproxiedMcpOverviewTab } from "./UnproxiedMcpOverviewTab";

// Picks the overview for the server's backend. Unproxied servers have no
// Gram-proxied traffic and get their own scoped-down tab; tunneled servers
// get the live connections panel on top of the shared usage dashboard.
export function OverviewTab({
  mcpServer,
  agentSetupHref,
}: {
  mcpServer: McpServer;
  agentSetupHref: string;
}): JSX.Element | null {
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
  // The usage dashboard is keyed by slug; a tunneled server without one still
  // gets its connections panel.
  const usage = mcpServer.slug ? (
    <MCPOverviewTab
      server={{
        kind: "mcp-server",
        id: mcpServer.id,
        slug: mcpServer.slug,
        name: mcpServer.name ?? "MCP Server",
      }}
    />
  ) : null;

  if (!mcpServer.tunneledMcpServerId) return usage;

  return (
    <Stack gap={6}>
      <TunneledMcpConnectionsPanel
        tunneledMcpServerId={mcpServer.tunneledMcpServerId}
        agentSetupHref={agentSetupHref}
      />
      {usage}
    </Stack>
  );
}
