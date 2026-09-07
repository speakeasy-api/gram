import { useIconConfetti } from "@/components/icon-confetti";
import { Card } from "@/components/ui/Card";
import { Text } from "@/components/ui/Text";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { Badge } from "@/components/ui/Badge";
import { ArrowRight } from "lucide-react";
import { Link } from "react-router";
import { SourceMcpIcon } from "@/components/sources/SourceCard";

// MCPServerCard renders an mcp_servers row inside the /mcp listing grid.
// Today only Remote-MCP-backed servers reach this component (filtered upstream
// by the remoteMcpServerId filter); after the AGE-1902/AGE-1880 cutover,
// toolset-backed mcp_servers will render through the same card alongside Hosted
// MCPCard.
//
// TODO(AGE-1902): collapse with MCPCard once Hosted (toolset-backed) cards
// also source from mcp_servers and the per-card data shape no longer branches
// on backend kind.
export function MCPServerCard({ server }: { server: McpServer }): JSX.Element {
  const routes = useRoutes();
  const { canvasRef, start, stop } = useIconConfetti();

  // How the server is reached, which is what distinguishes one mcp_servers row
  // from another on a page where they otherwise look identical.
  const kindLabel = server.unproxiedMcpServerId
    ? "Unproxied"
    : server.tunneledMcpServerId
      ? "Tunneled"
      : "Remote";

  return (
    <Link
      to={routes.mcp.x.overview.href(mcpServerRouteParam(server))}
      onMouseEnter={start}
      onMouseLeave={stop}
      className="focus-visible:ring-ring block h-full no-underline focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
    >
      <Card.Entity
        iconRailClassName="isolate"
        iconTileClassName="icon-hover-pulse"
        overlay={
          <canvas
            ref={canvasRef}
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 -z-10 size-full"
          />
        }
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
        </div>

        {/* mcp_servers rows carry no description, so the slug is what
            distinguishes two servers of the same kind — and it is what the
            URL on this line used to spell out. */}
        <Text small muted className="mt-1 truncate font-mono">
          {server.slug ?? "No slug yet"}
        </Text>

        {/* Footer row with status indicator and open link */}
        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <div className="flex items-center gap-2">
            {/* Every card names how the server is reached: the one thing that
                differs between them, and the reason they look alike. */}
            <Badge variant="neutral">
              <Badge.Text>{kindLabel}</Badge.Text>
            </Badge>
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
