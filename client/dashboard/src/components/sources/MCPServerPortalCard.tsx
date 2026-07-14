import { Card } from "@/components/ui/card";
import { StatusDot, type StatusDotTone } from "@/components/ui/status-dot";
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { Badge } from "@/components/ui/badge";
import { ArrowRight, Network } from "lucide-react";

type McpServerCardStatus = "public" | "private" | "disabled";

const MCP_SERVER_STATUS_PRESENTATION: Record<
  McpServerCardStatus,
  { label: string; tone: StatusDotTone; pulse: boolean }
> = {
  public: { label: "Public", tone: "success", pulse: true },
  private: { label: "Private", tone: "success", pulse: true },
  disabled: { label: "Disabled", tone: "destructive", pulse: false },
};

function mcpServerCardStatus(toolset: ToolsetEntry): McpServerCardStatus {
  if (!toolset.mcpEnabled) return "disabled";
  return toolset.mcpIsPublic ? "public" : "private";
}

/**
 * Card for an MCP server portal, keyed by the toolset backing it. Shared by
 * the Source detail "MCP Servers" tab (OpenAPI/function sources) and the
 * External MCP detail page — both list the toolsets a given source's tools
 * are exposed through.
 */
export function MCPServerPortalCard({
  toolset,
}: {
  toolset: ToolsetEntry;
}): JSX.Element {
  const routes = useRoutes();

  return (
    <routes.mcp.details.Link
      params={[toolset.slug]}
      className="hover:no-underline"
    >
      <Card icon={<Network className="text-muted-foreground h-10 w-10" />}>
        <div className="mb-1 flex items-start justify-between gap-2">
          <Type
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary truncate transition-colors"
          >
            {toolset.name}
          </Type>
          <Badge className="shrink-0">
            {`${toolset.toolUrns?.length || 0} tool${(toolset.toolUrns?.length || 0) !== 1 ? "s" : ""}`}
          </Badge>
        </div>
        <Type small muted className="truncate">
          {toolset.slug}
        </Type>
        {toolset.description && (
          <Type small muted className="mt-2 line-clamp-2">
            {toolset.description}
          </Type>
        )}
        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <StatusDot
            {...MCP_SERVER_STATUS_PRESENTATION[mcpServerCardStatus(toolset)]}
          />
          <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
            <span>Open</span>
            <ArrowRight className="h-3.5 w-3.5" />
          </div>
        </div>
      </Card>
    </routes.mcp.details.Link>
  );
}
