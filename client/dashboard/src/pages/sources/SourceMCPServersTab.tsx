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
      <DotCard icon={<Network className="text-muted-foreground h-10 w-10" />}>
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
          <div className="flex items-center gap-2">
            <div className="relative flex h-2.5 w-2.5">
              {toolset.mcpEnabled && (
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-green-400 opacity-75" />
              )}
              <span
                className={`relative inline-flex h-2.5 w-2.5 rounded-full ${toolset.mcpEnabled ? "bg-green-500" : "bg-red-500"}`}
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
          <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
            <span>Open</span>
            <ArrowRight className="h-3.5 w-3.5" />
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
    <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
      {associatedToolsets.length > 0 ? (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          {associatedToolsets.map((toolset) => (
            <MCPServerPortalCard key={toolset.slug} toolset={toolset} />
          ))}
        </div>
      ) : (
        <div className="rounded-lg border p-12 text-center">
          <Server className="text-muted-foreground mx-auto mb-3 h-10 w-10 opacity-40" />
          <Type className="mb-1 block font-medium">No MCP servers yet</Type>
          <Type muted small className="mx-auto mb-4 block max-w-sm">
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
