import { MCPServerPortalCard } from "@/components/sources/MCPServerPortalCard";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import { useRoutes } from "@/routes";
import { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { Button } from "@/components/ui/button";
import { Server } from "lucide-react";

export function SourceMCPServersTab({
  associatedToolsets,
}: {
  associatedToolsets: ToolsetEntry[];
}): JSX.Element {
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
        <InlineEmptyState
          className="py-12"
          icon={<Server />}
          title="No MCP servers yet"
          description="Create an MCP server that includes tools from this source to expose them to AI agents and clients."
          action={
            <routes.mcp.Link className="hover:no-underline">
              <Button variant="secondary" size="sm">
                <Button.LeftIcon>
                  <Server className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Go to MCP Servers</Button.Text>
              </Button>
            </routes.mcp.Link>
          }
        />
      )}
    </div>
  );
}
