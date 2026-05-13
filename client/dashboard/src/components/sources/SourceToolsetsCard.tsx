import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { Badge } from "@speakeasy-api/moonshine";
import { Package } from "lucide-react";

type ToolsetLike = {
  slug: string;
  name: string;
  description?: string;
  toolUrns?: string[];
};

interface SourceToolsetsCardProps {
  toolsetsUsingSource: ToolsetLike[];
  sourceToolUrns: Set<string>;
}

function ToolsetItem({
  toolset,
  sourceToolUrns,
}: {
  toolset: ToolsetLike;
  sourceToolUrns: Set<string>;
}) {
  const routes = useRoutes();

  const toolCount = toolset.toolUrns?.filter((urn) =>
    sourceToolUrns.has(urn),
  ).length;

  return (
    <routes.mcp.details.Link
      key={toolset.slug}
      params={[toolset.slug]}
      className="bg-surface-secondary hover:bg-surface-tertiary flex items-center justify-between rounded-md border p-3 transition-colors"
    >
      <div className="flex-1">
        <Type className="font-medium">{toolset.name}</Type>
        {toolset.description && (
          <Type className="text-muted-foreground mt-1 text-sm">
            {toolset.description}
          </Type>
        )}
      </div>
      <Badge variant="neutral" className="ml-2">
        {toolCount} {toolCount === 1 ? "tool" : "tools"}
      </Badge>
    </routes.mcp.details.Link>
  );
}

export function SourceToolsetsCard({
  toolsetsUsingSource,
  sourceToolUrns,
}: SourceToolsetsCardProps) {
  return (
    <div className="bg-card rounded-lg border p-6">
      <Type as="h2" className="mb-4 text-lg font-semibold">
        Used in MCP Servers ({toolsetsUsingSource.length})
      </Type>
      {toolsetsUsingSource.length === 0 ? (
        <div className="text-muted-foreground py-8 text-center">
          <Package className="mx-auto mb-3 h-12 w-12 opacity-50" />
          <Type>This source is not currently used in any MCP servers</Type>
        </div>
      ) : (
        <div className="space-y-2">
          {toolsetsUsingSource.map((toolset) => (
            <ToolsetItem
              key={toolset.slug}
              toolset={toolset}
              sourceToolUrns={sourceToolUrns}
            />
          ))}
        </div>
      )}
    </div>
  );
}
