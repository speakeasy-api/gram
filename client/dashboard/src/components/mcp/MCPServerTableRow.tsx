import { DotRow } from "@/components/ui/dot-row";
import { Type } from "@/components/ui/type";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components";
import { Network } from "lucide-react";
import { Badge } from "../ui/badge";
import { MCPStatusIndicator } from "./MCPStatusIndicator";

// MCPServerTableRow renders an mcp_servers row in the /mcp listing table
// view. Mirrors MCPTableRow.
//
// TODO(AGE-1902): collapse with MCPTableRow once Hosted (toolset-backed) rows
// also source from mcp_servers and the per-row data shape no longer branches
// on backend kind.
export function MCPServerTableRow({
  server,
  endpointCount,
}: {
  server: McpServer;
  endpointCount: number;
}) {
  const routes = useRoutes();

  const handleClick = () => {
    routes.mcp.x.goTo(mcpServerRouteParam(server));
  };

  const mcpEnabled = server.visibility !== "disabled";
  const mcpIsPublic = server.visibility === "public";

  return (
    <DotRow
      onClick={handleClick}
      icon={<Network className="text-muted-foreground h-5 w-5" />}
    >
      {/* Name */}
      <td className="px-3 py-3">
        <Type
          variant="subheading"
          as="div"
          className="group-hover:text-primary truncate text-sm transition-colors"
          title={server.name ?? undefined}
        >
          {server.name || "MCP Server"}
        </Type>
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
        <Type small muted>
          {endpointCount} {endpointCount === 1 ? "endpoint" : "endpoints"}
        </Type>
      </td>

      {/* Tools column slot — mcp_servers don't expose tool catalogs through Gram today */}
      <td className="px-3 py-3">
        <Badge variant="outline">MCP Server</Badge>
      </td>
    </DotRow>
  );
}
