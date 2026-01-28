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
      className="flex items-center justify-between p-3 rounded-md border bg-surface-secondary hover:bg-surface-tertiary transition-colors"
    >
      <div className="flex-1">
        <Type className="font-medium">{toolset.name}</Type>
        {toolset.description && (
          <Type className="text-sm text-muted-foreground mt-1">
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
    <div className="rounded-lg border bg-card p-6">
      <Type as="h2" className="text-lg font-semibold mb-4">
        Used in Toolsets ({toolsetsUsingSource.length})
      </Type>
      {toolsetsUsingSource.length === 0 ? (
        <div className="text-center py-8 text-muted-foreground">
          <Package className="h-12 w-12 mx-auto mb-3 opacity-50" />
          <Type>This source is not currently used in any toolsets</Type>
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
