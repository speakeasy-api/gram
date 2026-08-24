import { DotRow } from "@/components/ui/DotRow";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { Network } from "lucide-react";
import { Badge } from "../ui/Badge";

// GatewayTableRow renders a meta MCP server (Gateway Endpoint) in the /mcp
// listing table view. Mirrors MCPServerTableRow.
export function GatewayTableRow({
  gateway,
  endpointCount,
  url,
}: {
  gateway: MetaMcpServer;
  endpointCount: number;
  url: string | undefined;
}): JSX.Element {
  const routes = useRoutes();

  return (
    <DotRow
      onClick={() => routes.mcp.gateway.overview.goTo(gateway.id)}
      icon={<Network className="text-muted-foreground h-5 w-5" />}
    >
      {/* Name */}
      <td className="px-3 py-3">
        <Text
          variant="subheading"
          as="div"
          className="group-hover:text-primary min-w-0 flex-1 truncate text-sm transition-colors"
          title={gateway.name}
        >
          {gateway.name}
        </Text>
      </td>

      {/* Visibility column slot: gateways have no visibility setting, so this
          reports the closest equivalent — whether callers must sign in. */}
      <td className="px-3 py-3">
        <Text small muted>
          {gateway.userSessionIssuerId ? "Requires sign-in" : "Open"}
        </Text>
      </td>

      {/* URL column slot */}
      <td className="px-3 py-3">
        <Text small muted className="truncate font-mono text-xs">
          {url
            ? url.replace(/^https?:\/\//, "")
            : `${endpointCount} ${endpointCount === 1 ? "endpoint" : "endpoints"}`}
        </Text>
      </td>

      {/* Tools column slot */}
      <td className="px-3 py-3">
        <Badge variant="neutral">Gateway</Badge>
      </td>
    </DotRow>
  );
}
