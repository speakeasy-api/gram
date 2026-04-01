import { DotCard } from "@/components/ui/dot-card";
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { ArrowRight, Network, Server } from "lucide-react";

function MCPServerPortalCard({ toolset }: { toolset: ToolsetEntry }) {
  const routes = useRoutes();

  return (
    <routes.mcp.details.Link
      params={[toolset.slug]}
      className="hover:no-underline"
    >
      <DotCard icon={<Network className="w-10 h-10 text-muted-foreground" />}>
        <div className="flex items-start justify-between gap-2 mb-1">
          <Type
            variant="subheading"
            as="div"
            className="truncate text-md group-hover:text-primary transition-colors"
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
          <Type small muted className="line-clamp-2 mt-2">
            {toolset.description}
          </Type>
        )}
        <div className="flex items-center justify-between gap-2 mt-auto pt-2">
          <div className="flex items-center gap-2">
            <div className="relative flex h-2.5 w-2.5">
              {toolset.mcpEnabled && (
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full opacity-75 bg-green-400" />
              )}
              <span
                className={`relative inline-flex rounded-full h-2.5 w-2.5 ${toolset.mcpEnabled ? "bg-green-500" : "bg-red-500"}`}
              />
            </div>
            <Type variant="small" muted>
              {toolset.mcpEnabled
                ? toolset.mcpIsPublic
                  ? "Public"
                  : "Private"
                : "Disabled"}
            </Type>
          </div>
          <div className="flex items-center gap-1 text-muted-foreground group-hover:text-primary transition-colors text-sm">
            <span>Open</span>
            <ArrowRight className="w-3.5 h-3.5" />
          </div>
        </div>
      </DotCard>
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
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
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
