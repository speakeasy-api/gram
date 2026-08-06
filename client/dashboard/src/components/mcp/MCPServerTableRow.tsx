import { DotRow } from "@/components/ui/DotRow";
import { Text } from "@/components/ui/Text";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { Badge } from "../ui/Badge";
import { MCPStatusIndicator } from "./MCPStatusIndicator";
import { MCPActivityIndicator } from "./MCPActivityIndicator";
import type { McpActivityStatus } from "./mcp-activity";

// MCPServerTableRow renders an mcp_servers row in the /mcp listing table
// view. Mirrors MCPTableRow.
//
// TODO(AGE-1902): collapse with MCPTableRow once Hosted (toolset-backed) rows
// also source from mcp_servers and the per-row data shape no longer branches
// on backend kind.
export function MCPServerTableRow({
  server,
  endpointCount,
  activityStatus,
  recentWindowDays,
}: {
  server: McpServer;
  endpointCount: number;
  activityStatus?: McpActivityStatus | null;
  recentWindowDays?: number;
}): JSX.Element {
  const routes = useRoutes();

  const handleClick = () => {
    routes.mcp.x.overview.goTo(mcpServerRouteParam(server));
  };

  const mcpEnabled = server.visibility !== "disabled";
  const mcpIsPublic = server.visibility === "public";
  const isUnproxied = !!server.unproxiedMcpServerId;

  return (
    <DotRow
      onClick={handleClick}
      icon={
        <SourceMcpIcon
          mcpServerId={server.id}
          className="h-5 w-5 object-contain"
        />
      }
    >
      {/* Name */}
      <td className="px-3 py-3">
        <div className="flex items-center gap-2">
          <Text
            variant="subheading"
            as="div"
            className="group-hover:text-primary min-w-0 flex-1 truncate text-sm transition-colors"
            title={server.name ?? undefined}
          >
            {server.name || "MCP Server"}
          </Text>
          {activityStatus && (
            <MCPActivityIndicator
              status={activityStatus}
              recentWindowDays={recentWindowDays}
              size="sm"
              className="shrink-0"
            />
          )}
        </div>
      </td>

      {/* Status */}
      <td className="px-3 py-3">
        <MCPStatusIndicator
          mcpEnabled={mcpEnabled}
          mcpIsPublic={mcpIsPublic}
          size="sm"
        />
      </td>

      {/* URL column slot — endpoint count for mcp_servers-backed rows */}
      <td className="px-3 py-3">
        <Text small muted>
          {isUnproxied
            ? "Not proxied"
            : `${endpointCount} ${endpointCount === 1 ? "endpoint" : "endpoints"}`}
        </Text>
      </td>

      {/* Tools column slot — mcp_servers don't expose tool catalogs through Gram today */}
      <td className="px-3 py-3">
        <Badge variant="neutral">MCP Server</Badge>
      </td>
    </DotRow>
  );
}
