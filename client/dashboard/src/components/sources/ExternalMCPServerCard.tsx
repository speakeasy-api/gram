import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { Badge } from "@speakeasy-api/moonshine";
import { ChevronRight, Globe, Lock, Power, Server } from "lucide-react";

interface ExternalMCPServerCardProps {
  toolset: ToolsetEntry;
  isLoading?: boolean;
}

export function ExternalMCPServerCard({ toolset }: ExternalMCPServerCardProps) {
  const routes = useRoutes();

  return (
    <div className="bg-card rounded-lg border p-6">
      <Type as="h2" className="mb-4 text-lg font-semibold">
        MCP Server
      </Type>

      <Type className="text-muted-foreground mb-4 text-sm">
        This external MCP source is exposed through a single MCP server. Changes
        to the source will be reflected in this server.
      </Type>

      {/* MCP Server Card - Clickable */}
      <routes.mcp.details.Link
        params={[toolset.slug]}
        className="hover:bg-surface-secondary block cursor-pointer  rounded-md border transition-colors hover:no-underline"
      >
        <div className="p-4">
          {/* Header Section */}
          <div className="flex items-center justify-between gap-3">
            <div className="flex-1">
              <div className="mb-1 flex items-center gap-2">
                <Type className="text-base font-semibold">{toolset.name}</Type>
                <McpEnabledBadge enabled={!!toolset.mcpEnabled} />
                <McpPublicBadge isPublic={!!toolset.mcpIsPublic} />
              </div>
              {toolset.description && (
                <Type className="text-muted-foreground text-sm">
                  {toolset.description}
                </Type>
              )}
            </div>
            <ChevronRight className="text-muted-foreground h-5 w-5 shrink-0" />
          </div>
        </div>
      </routes.mcp.details.Link>
    </div>
  );
}

export function ExternalMCPServerCardLoading() {
  return (
    <div className="bg-card rounded-lg border p-6">
      <Type as="h2" className="mb-4 text-lg font-semibold">
        MCP Server
      </Type>
      <div className="text-muted-foreground py-8 text-center">
        <Server className="mx-auto mb-3 h-12 w-12 animate-pulse opacity-50" />
        <Type>Loading MCP server information...</Type>
      </div>
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
