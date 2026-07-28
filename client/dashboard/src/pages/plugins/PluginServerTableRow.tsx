import { MCPStatusIndicator } from "@/components/mcp/MCPStatusIndicator";
import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { Button } from "@/components/ui/button";
import { DotRow } from "@/components/ui/dot-row";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { PluginServer } from "@gram/client/models/components/pluginserver.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { Badge } from "@speakeasy-api/moonshine";
import { Network, Trash2 } from "lucide-react";

export function PluginServerTableRow({
  server,
  toolset,
  mcpServer,
  isLoading,
  onRemove,
  lastPublishedAt,
}: {
  server: PluginServer;
  toolset: ToolsetEntry | undefined;
  mcpServer: McpServer | undefined;
  isLoading: boolean;
  onRemove: () => void;
  lastPublishedAt: Date | undefined;
}): JSX.Element {
  const routes = useRoutes();
  const isRemote = !!server.mcpServerId;
  const notYetPublished =
    !lastPublishedAt || server.createdAt > lastPublishedAt;

  let href: string | undefined;
  if (isRemote && mcpServer) {
    href = routes.mcp.x.overview.href(mcpServerRouteParam(mcpServer));
  } else if (toolset) {
    href = routes.mcp.details.href(toolset.slug);
  }

  let typeContent: JSX.Element;
  if (isRemote) {
    typeContent = <Badge variant="neutral">Remote MCP</Badge>;
  } else if (toolset) {
    typeContent = (
      <ToolCollectionBadge toolNames={toolset.tools.map((tool) => tool.name)} />
    );
  } else if (isLoading) {
    typeContent = <Skeleton className="h-5 w-16" />;
  } else {
    typeContent = <Badge variant="destructive">Toolset missing</Badge>;
  }

  let visibilityContent: JSX.Element;
  if (isRemote) {
    visibilityContent = (
      <Type small muted>
        —
      </Type>
    );
  } else if (toolset) {
    visibilityContent = (
      <MCPStatusIndicator
        mcpEnabled={toolset.mcpEnabled}
        mcpIsPublic={toolset.mcpIsPublic}
        size="sm"
      />
    );
  } else if (isLoading) {
    visibilityContent = <Skeleton className="h-3.5 w-20" />;
  } else {
    visibilityContent = (
      <Type small muted>
        —
      </Type>
    );
  }

  return (
    <DotRow
      icon={<Network className="text-muted-foreground h-5 w-5" />}
      href={href}
      ariaLabel={`View server ${server.displayName}`}
    >
      <td className="px-3 py-3">
        <Type
          variant="subheading"
          as="div"
          className="group-hover:text-primary truncate text-sm transition-colors"
          title={server.displayName}
        >
          {server.displayName}
        </Type>
      </td>
      <td className="px-3 py-3">
        {notYetPublished ? (
          <Badge
            variant="warning"
            title="Added since the marketplace was last published"
          >
            Unpublished
          </Badge>
        ) : (
          <Badge variant="success">Published</Badge>
        )}
      </td>
      <td className="px-3 py-3">{typeContent}</td>
      <td className="px-3 py-3">{visibilityContent}</td>
      <td className="px-3 py-3">
        <div
          className="relative z-20 flex items-center justify-end"
          onClick={(event) => event.stopPropagation()}
        >
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            tooltip="Remove server"
            aria-label={`Remove server ${server.displayName}`}
            className="hover:text-destructive"
            onClick={onRemove}
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </td>
    </DotRow>
  );
}
