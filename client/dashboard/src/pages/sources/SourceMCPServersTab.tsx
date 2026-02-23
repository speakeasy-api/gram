import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { ChevronRight, Globe, Lock, Power, Server } from "lucide-react";

function McpEnabledBadge({ enabled }: { enabled: boolean }) {
  return (
    <Badge variant={enabled ? "success" : "neutral"} className="gap-1">
      <Power size={12} />
      <Badge.Text>{enabled ? "Enabled" : "Disabled"}</Badge.Text>
    </Badge>
  );
}

function McpPublicBadge({ isPublic }: { isPublic: boolean }) {
  return (
    <Badge variant={isPublic ? "success" : "neutral"} className="gap-1">
      {isPublic ? <Globe size={12} /> : <Lock size={12} />}
      <Badge.Text>{isPublic ? "Public" : "Private"}</Badge.Text>
    </Badge>
  );
}

function MCPServerPortalCard({ toolset }: { toolset: ToolsetEntry }) {
  const routes = useRoutes();

  return (
    <routes.mcp.details.Link
      params={[toolset.slug]}
      className="group block rounded-xl border bg-card hover:bg-surface-secondary hover:border-primary/30 transition-all duration-200 cursor-pointer hover:no-underline hover:shadow-lg"
    >
      <div className="p-5">
        <div className="flex items-start justify-between gap-3 mb-3">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-lg bg-primary/10 flex items-center justify-center">
              <Server className="h-5 w-5 text-primary" />
            </div>
            <div>
              <Type className="font-semibold text-base group-hover:text-primary transition-colors">
                {toolset.name}
              </Type>
              <div className="flex items-center gap-2 mt-1">
                <McpEnabledBadge enabled={!!toolset.mcpEnabled} />
                <McpPublicBadge isPublic={!!toolset.mcpIsPublic} />
              </div>
            </div>
          </div>
          <ChevronRight className="h-5 w-5 text-muted-foreground group-hover:text-primary group-hover:translate-x-0.5 transition-all shrink-0 mt-2" />
        </div>

        {toolset.description && (
          <Type className="text-sm text-muted-foreground line-clamp-2">
            {toolset.description}
          </Type>
        )}

        <div className="mt-4 pt-3 border-t">
          <Type className="text-xs text-muted-foreground">
            {toolset.toolUrns?.length || 0} tool
            {(toolset.toolUrns?.length || 0) !== 1 ? "s" : ""} available
          </Type>
        </div>
      </div>
    </routes.mcp.details.Link>
  );
}

export function SourceMCPServersTab({
  associatedToolsets,
}: {
  associatedToolsets: ToolsetEntry[];
}) {
  const routes = useRoutes();

  return (
    <div className="max-w-[1270px] mx-auto px-8 py-8 w-full">
      {associatedToolsets.length > 0 ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {associatedToolsets.map((toolset) => (
            <MCPServerPortalCard key={toolset.slug} toolset={toolset} />
          ))}
        </div>
      ) : (
        <div className="border rounded-lg p-12 text-center">
          <Server className="h-10 w-10 text-muted-foreground mx-auto mb-3 opacity-40" />
          <Type className="block mb-1 font-medium">No MCP servers yet</Type>
          <Type muted small className="block max-w-sm mx-auto mb-4">
            Create an MCP server that includes tools from this source to expose
            them to AI agents and clients.
          </Type>
          <routes.mcp.Link className="hover:no-underline">
            <Button variant="secondary" size="sm">
              <Button.LeftIcon>
                <Server className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Go to MCP Servers</Button.Text>
            </Button>
          </routes.mcp.Link>
        </div>
      )}
    </div>
  );
}
