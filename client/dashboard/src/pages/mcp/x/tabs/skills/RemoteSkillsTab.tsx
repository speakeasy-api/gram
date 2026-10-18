import { RequireScope } from "@/components/require-scope";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { useProject } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { ServedSkillsSection } from "@/pages/mcp/MCPSkillsTab";
import { mcpServerTabHref } from "@/pages/mcp/x/MCPServerDetailsRouting";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { Navigate } from "react-router";

/**
 * The Skills tab of a remote MCP server. Like the hosted server's tab it only
 * exists while skills over MCP is on for the organization, and the flag is
 * decided before the scope so a direct link with the flag off lands on the
 * overview. Only remote-backed servers have the tab.
 */
export function RemoteSkillsTabRoute({
  mcpServer,
  mcpServerSlug,
}: {
  mcpServer: McpServer;
  mcpServerSlug: string;
}): JSX.Element {
  const routes = useRoutes();
  const project = useProject();
  const flag = useFeatureFlag(FEATURE_FLAGS.skillsOverMcp);
  const overview = (
    <Navigate
      to={mcpServerTabHref(routes, mcpServerSlug, "overview")}
      replace
    />
  );
  if (!mcpServer.remoteMcpServerId) return overview;
  switch (flag.status) {
    case "enabled":
      return (
        <RequireScope
          scope="skill:read"
          resourceId={project.id}
          projectId={project.id}
          level="page"
        >
          <ServedSkillsSection
            server={{
              kind: "remote",
              mcpServerId: mcpServer.id,
              name: mcpServer.name || "this server",
            }}
          />
        </RequireScope>
      );
    case "loading":
      return <SkeletonTable />;
    case "disabled":
    case "missing":
    case "error":
      return overview;
  }
}
