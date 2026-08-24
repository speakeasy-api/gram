import { Badge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { ArrowRight, Network } from "lucide-react";
import { Link } from "react-router";

// GatewayCard renders a meta MCP server (Gateway Endpoint) inside the /mcp
// listing grid, alongside MCPCard (toolsets) and MCPServerCard (mcp_servers).
export function GatewayCard({
  gateway,
  endpointCount,
  url,
}: {
  gateway: MetaMcpServer;
  endpointCount: number;
  /** Canonical address, when the gateway has one. */
  url: string | undefined;
}): JSX.Element {
  const routes = useRoutes();

  return (
    <Link
      to={routes.mcp.gateway.overview.href(gateway.id)}
      className="focus-visible:ring-ring block no-underline focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
    >
      <Card.Entity icon={<Network className="text-muted-foreground h-8 w-8" />}>
        <div className="mb-2 flex items-start justify-between gap-2">
          <Text
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary flex-1 truncate transition-colors"
            title={gateway.name}
          >
            {gateway.name}
          </Text>
          <Badge variant="neutral" className="bg-card">
            <Badge.Text>
              {`${endpointCount} ${endpointCount === 1 ? "endpoint" : "endpoints"}`}
            </Badge.Text>
          </Badge>
        </div>

        {url ? (
          <div
            className="flex items-center gap-1"
            // The card is a link; let the copy button act without navigating.
            onClick={(e) => e.preventDefault()}
          >
            <Text muted className="truncate font-mono text-xs">
              {url.replace(/^https?:\/\//, "")}
            </Text>
            <CopyButton text={url} size="xs" tooltip="Copy URL" />
          </div>
        ) : (
          <Text muted className="text-xs">
            No address yet
          </Text>
        )}

        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <div className="flex items-center gap-2">
            <Badge variant="neutral">
              <Badge.Text>Gateway</Badge.Text>
            </Badge>
            <Text muted className="text-xs">
              {gateway.userSessionIssuerId
                ? "Requires sign-in"
                : "Open to anyone with the URL"}
            </Text>
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
