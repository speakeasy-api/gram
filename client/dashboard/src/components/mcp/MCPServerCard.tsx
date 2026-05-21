import { DotCard } from "@/components/ui/dot-card";
import { Type } from "@/components/ui/type";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components";
import { ArrowRight, Network } from "lucide-react";
import { Link } from "react-router";
import { Badge } from "../ui/badge";
import { MCPStatusIndicator } from "./MCPStatusIndicator";

// MCPServerCard renders an mcp_servers row inside the /mcp listing grid.
// Today only Remote-MCP-backed servers reach this component (gated upstream
// by the gram-remote-mcp flag and the remoteMcpServerId filter); after the
// AGE-1902/AGE-1880 cutover, toolset-backed mcp_servers will render through
// the same card alongside Hosted MCPCard.
//
// TODO(AGE-1902): collapse with MCPCard once Hosted (toolset-backed) cards
// also source from mcp_servers and the per-card data shape no longer branches
// on backend kind.
export function MCPServerCard({
  server,
  endpointCount,
}: {
  server: McpServer;
  endpointCount: number;
}) {
  const routes = useRoutes();

  const mcpEnabled = server.visibility !== "disabled";
  const mcpIsPublic = server.visibility === "public";

  return (
    <Link
      to={routes.mcp.x.href(mcpServerRouteParam(server))}
      className="focus-visible:ring-ring block rounded-xl no-underline focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
    >
      <DotCard icon={<Network className="text-muted-foreground h-8 w-8" />}>
        {/* Header row with name */}
        <div className="mb-2 flex items-start justify-between gap-2">
          <Type
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary flex-1 truncate transition-colors"
            title={server.name ?? undefined}
          >
            {server.name || "MCP Server"}
          </Type>
          <Badge variant="outline">
            {endpointCount} {endpointCount === 1 ? "endpoint" : "endpoints"}
          </Badge>
        </div>

        {/* Footer row with status indicator and open link */}
        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <MCPStatusIndicator
            mcpEnabled={mcpEnabled}
            mcpIsPublic={mcpIsPublic}
          />
          <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
            <span>Open</span>
            <ArrowRight className="h-3.5 w-3.5" />
          </div>
        </div>
      </DotCard>
    </Link>
  );
}

export function MCPServerCardSkeleton() {
  return (
    <DotCard>
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="bg-muted h-5 w-2/3 animate-pulse rounded" />
        <div className="bg-muted h-5 w-20 animate-pulse rounded-full" />
      </div>
      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <div className="flex items-center gap-2">
          <div className="bg-muted h-2.5 w-2.5 animate-pulse rounded-full" />
          <div className="bg-muted h-3.5 w-12 animate-pulse rounded" />
        </div>
        <div className="bg-muted h-3.5 w-10 animate-pulse rounded" />
      </div>
    </DotCard>
  );
}
