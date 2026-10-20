import { Alert } from "@/components/ui/Alert";
import { mcpServerTabHref } from "@/pages/mcp/x/MCPServerDetailsRouting";
import {
  ServedSkillsSection,
  SkillsOverMcpTabRoute,
} from "@/pages/skills/ServedSkillsSection";
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
  mcpServer: Pick<McpServer, "id" | "name" | "remoteMcpServerId">;
  mcpServerSlug: string;
}): JSX.Element {
  const routes = useRoutes();
  const overviewHref = mcpServerTabHref(routes, mcpServerSlug, "overview");
  if (!mcpServer.remoteMcpServerId) {
    return <Navigate to={overviewHref} replace />;
  }
  return (
    <SkillsOverMcpTabRoute fallbackHref={overviewHref}>
      <RemoteSkillsTab mcpServer={mcpServer} />
    </SkillsOverMcpTabRoute>
  );
}

/**
 * The skills linked to a remote server's mcp_servers row. The remote URL
 * proxies everything but tools to the upstream, so these links are served by
 * the gateways that include the server, not by the server's own URL.
 */
export function RemoteSkillsTab({
  mcpServer,
}: {
  mcpServer: Pick<McpServer, "id" | "name">;
}): JSX.Element {
  return (
    <ServedSkillsSection
      target={{ kind: "mcp_server", mcpServerId: mcpServer.id }}
      surfaceName={mcpServer.name || "this server"}
      notice={
        <Alert variant="info" alignTop className="mb-4 text-sm">
          The remote server&apos;s own skills pass through to agents unchanged.
          Skills added here aren&apos;t served at this server&apos;s own URL;
          agents load them through gateways that include this server.
        </Alert>
      }
    />
  );
}
