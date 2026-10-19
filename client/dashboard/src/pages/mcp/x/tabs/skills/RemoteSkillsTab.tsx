import { ServedSkillsSection, SkillsTabGate } from "@/pages/mcp/MCPSkillsTab";
import { mcpServerTabHref } from "@/pages/mcp/x/MCPServerDetailsRouting";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { Navigate } from "react-router";

/**
 * The Skills tab of a remote MCP server, behind the same gate as the hosted
 * server's tab. Only remote-backed servers have the tab; any other server,
 * or a direct link with the flag off, lands on the overview.
 */
export function RemoteSkillsTabRoute({
  mcpServer,
  mcpServerSlug,
}: {
  mcpServer: McpServer;
  mcpServerSlug: string;
}): JSX.Element {
  const routes = useRoutes();
  const overviewHref = mcpServerTabHref(routes, mcpServerSlug, "overview");
  if (!mcpServer.remoteMcpServerId) {
    return <Navigate to={overviewHref} replace />;
  }
  return (
    <SkillsTabGate fallbackHref={overviewHref}>
      <ServedSkillsSection
        server={{
          kind: "remote",
          mcpServerId: mcpServer.id,
          name: mcpServer.name || "this server",
        }}
      />
    </SkillsTabGate>
  );
}
