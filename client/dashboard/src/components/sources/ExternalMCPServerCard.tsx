import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { Badge } from "@speakeasy-api/moonshine";
import { ChevronRight, Globe, Lock, Power, Server } from "lucide-react";

interface ExternalMCPServerCardProps {
  toolset: ToolsetEntry | undefined;
  isLoading?: boolean;
}

export function ExternalMCPServerCard({
  toolset,
  isLoading = false,
}: ExternalMCPServerCardProps) {
  const routes = useRoutes();

  if (isLoading || !toolset) {
    return (
      <div className="rounded-lg border bg-card p-6">
        <Type as="h2" className="text-lg font-semibold mb-4">
          MCP Server
        </Type>
        <div className="text-center py-8 text-muted-foreground">
          <Server className="h-12 w-12 mx-auto mb-3 opacity-50 animate-pulse" />
          <Type>Loading MCP server information...</Type>
        </div>
      </div>
    );
  }

  return (
    <div className="rounded-lg border bg-card p-6">
      <Type as="h2" className="text-lg font-semibold mb-4">
        MCP Server
      </Type>

      <Type className="text-sm text-muted-foreground mb-4">
        This external MCP source is exposed through a single MCP server. Changes
        to the source will be reflected in this server.
      </Type>

      {/* MCP Server Card - Clickable */}
      <routes.mcp.details.Link
        params={[toolset.slug]}
        className="block rounded-md border  hover:bg-surface-secondary transition-colors cursor-pointer hover:no-underline"
      >
        <div className="p-4">
          {/* Header Section */}
          <div className="flex justify-between gap-3 items-center">
            <div className="flex-1">
              <div className="flex items-center gap-2 mb-1">
                <Type className="font-semibold text-base">{toolset.name}</Type>
                <McpEnabledBadge enabled={!!toolset.mcpEnabled} />
                <McpPublicBadge isPublic={!!toolset.mcpIsPublic} />
              </div>
              {toolset.description && (
                <Type className="text-sm text-muted-foreground">
                  {toolset.description}
                </Type>
              )}
            </div>
            <ChevronRight className="h-5 w-5 text-muted-foreground shrink-0" />
          </div>
        </div>
      </routes.mcp.details.Link>
    </div>
  );
}

function McpEnabledBadge({ enabled }: { enabled: boolean }) {
  if (enabled) {
    return (
      <Badge variant="success" className="gap-1">
        <Power size={12} />
        Enabled
      </Badge>
    );
  }

  return (
    <Badge variant="neutral" className="gap-1">
      <Power size={12} />
      Disabled
    </Badge>
  );
}

function McpPublicBadge({ isPublic }: { isPublic: boolean }) {
  if (isPublic) {
    return (
      <Badge variant="success" className="gap-1">
        <Globe size={12} />
        Public
      </Badge>
    );
  }

  return (
    <Badge variant="neutral" className="gap-1">
      <Lock size={12} />
      Private
    </Badge>
  );
}
