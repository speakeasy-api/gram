import { Text } from "@/components/ui/Text";
import { mcpServerTabHref } from "@/pages/mcp/x/MCPServerDetailsRouting";
import {
  ServedSkillsSection,
  SkillsOverMcpTabRoute,
} from "@/pages/skills/ServedSkillsSection";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";

type SkillsTabServer = Pick<
  McpServer,
  "id" | "name" | "toolsetId" | "visibility"
>;

/**
 * The route behind a hosted MCP server's Skills tab: only while skills over
 * MCP is on, and only for readers of this project's skills. Servers that are
 * not hosted never reach it; the details page redirects them to the overview.
 */
export function MCPSkillsTabRoute({
  mcpServer,
  mcpServerSlug,
}: {
  mcpServer: SkillsTabServer;
  mcpServerSlug: string;
}): JSX.Element {
  const routes = useRoutes();
  return (
    <SkillsOverMcpTabRoute
      fallbackHref={mcpServerTabHref(routes, mcpServerSlug, "overview")}
    >
      <MCPSkillsTab mcpServer={mcpServer} />
    </SkillsOverMcpTabRoute>
  );
}

/**
 * The skills agents load when they connect to this hosted MCP server. They
 * are the skills linked to this server's own row; other servers built on the
 * same toolset keep their own links.
 */
export function MCPSkillsTab({
  mcpServer,
}: {
  mcpServer: SkillsTabServer;
}): JSX.Element {
  return (
    <ServedSkillsSection
      target={{ kind: "mcp_server", mcpServerId: mcpServer.id }}
      // A hosted server is granted under its toolset id, like the API checks it.
      grantResourceId={mcpServer.toolsetId ?? mcpServer.id}
      surfaceName={mcpServer.name || "this server"}
      notice={
        mcpServer.visibility === "disabled" ? (
          <Text small className="text-warning mb-3 block">
            This server is disabled, so no agent can load these skills until you
            enable it.
          </Text>
        ) : null
      }
    />
  );
}
