import { Card } from "@/components/ui/Card";
import { Text } from "@/components/ui/Text";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { Badge } from "@/components/ui/Badge";
import { ArrowRight } from "lucide-react";
import { Link } from "react-router";
import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { MCPStatusIndicator } from "./MCPStatusIndicator";
import { MCPActivityIndicator } from "./MCPActivityIndicator";
import type { McpActivityStatus } from "./mcp-activity";

// MCPServerCard renders an mcp_servers row inside the /mcp listing grid.
// Today only Remote-MCP-backed servers reach this component (filtered upstream
// by the remoteMcpServerId filter); after the AGE-1902/AGE-1880 cutover,
// toolset-backed mcp_servers will render through the same card alongside Hosted
// MCPCard.
//
// TODO(AGE-1902): collapse with MCPCard once Hosted (toolset-backed) cards
// also source from mcp_servers and the per-card data shape no longer branches
// on backend kind.
export function MCPServerCard({
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

  const mcpEnabled = server.visibility !== "disabled";
  const mcpIsPublic = server.visibility === "public";
  const mcpIsUpstream = server.visibility === "upstream";
  // Unproxied servers are never proxied, so an endpoint count would always
  // read 0 and imply something's broken. Surface the backend kind instead.
  const isUnproxied = !!server.unproxiedMcpServerId;

  return (
    <Link
      to={routes.mcp.x.overview.href(mcpServerRouteParam(server))}
      className="focus-visible:ring-ring block no-underline focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
    >
      <Card.Entity
        icon={
          <SourceMcpIcon
            mcpServerId={server.id}
            className="h-8 w-8 object-contain"
          />
        }
      >
        {/* Header row with name */}
        <div className="mb-2 flex items-start justify-between gap-2">
          <Text
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary flex-1 truncate transition-colors"
            title={server.name ?? undefined}
          >
            {server.name || "MCP Server"}
          </Text>
          <Badge variant="neutral" className="bg-card">
            <Badge.Text>
              {isUnproxied
                ? "Not proxied"
                : `${endpointCount} ${endpointCount === 1 ? "endpoint" : "endpoints"}`}
            </Badge.Text>
          </Badge>
        </div>

        {/* Footer row with status indicator and open link */}
        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <div className="flex items-center gap-2">
            <MCPStatusIndicator
              mcpEnabled={mcpEnabled}
              mcpIsPublic={mcpIsPublic}
              mcpIsUpstream={mcpIsUpstream}
            />
            {activityStatus && (
              <MCPActivityIndicator
                status={activityStatus}
                recentWindowDays={recentWindowDays}
              />
            )}
          </div>
          <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
            <span>Open</span>
            <ArrowRight className="h-3.5 w-3.5" />
          </div>
        </div>
      </Card.Entity>
    </Link>
  );
}
