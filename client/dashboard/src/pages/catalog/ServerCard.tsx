import {
  ExternalMCPIllustration,
  MCPPatternIllustration,
} from "@/components/sources/SourceCardIllustrations";
import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { Badge } from "@/components/ui/badge";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import type { DeploymentExternalMCP } from "@gram/client/models/components";
import { Button } from "@speakeasy-api/moonshine";
import { Loader2, Minus, Plus } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router";
import type { Server } from "./hooks";
import { parseServerMetadata } from "./hooks/serverMetadata";

interface ServerCardProps {
  server: Server;
  detailHref: string;
  externalMcps: DeploymentExternalMCP[];
  onAdd: () => void;
  onRemove: (slug: string) => void;
  isRemoving: boolean;
}

/**
 * Server card matching the MCPCard design pattern.
 *
 * Features:
 * - Pattern illustration header with logo overlay
 * - Tool count badge
 * - Official badge
 * - Monthly users count
 * - Add/Remove actions
 */
export function ServerCard({
  server,
  detailHref,
  externalMcps,
  onAdd,
  onRemove,
  isRemoving,
}: ServerCardProps) {
  const metadata = useMemo(() => parseServerMetadata(server), [server]);
  const displayName = server.title ?? server.registrySpecifier;

  const existingMcp = externalMcps.find(
    (mcp) => mcp.registryServerSpecifier === server.registrySpecifier,
  );
  const isAdded = !!existingMcp;

  // Generate a slug from the registry specifier for pattern generation
  const slug = server.registrySpecifier.replace(/[/@]/g, "-");

  // Get tool names for the badge tooltip
  const toolNames = useMemo(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const tools = (server.tools ?? []) as any[];
    return tools.map((t) => t.name || "Unknown tool");
  }, [server.tools]);

  return (
    <Link to={detailHref}>
      <div
        className={cn(
          "group bg-card text-card-foreground flex flex-col rounded-xl border overflow-hidden",
          "hover:border-foreground/20 hover:shadow-md transition-all cursor-pointer h-full",
          isAdded && "border-success/50 ring-1 ring-success/20",
        )}
      >
        {/* Illustration header */}
        <div className="h-32 w-full overflow-hidden border-b relative">
          {server.iconUrl ? (
            <ExternalMCPIllustration
              slug={slug}
              logoUrl={server.iconUrl}
              name={displayName}
            />
          ) : (
            <MCPPatternIllustration
              toolsetSlug={slug}
              className="saturate-[.3] group-hover:saturate-100 transition-all duration-300"
            />
          )}
          {/* Official badge overlay */}
          {metadata.isOfficial && (
            <div className="absolute top-2 right-2">
              <Badge
                variant="outline"
                className="border-warning/50 bg-warning/10 text-warning backdrop-blur-sm"
              >
                Official
              </Badge>
            </div>
          )}
          {/* Added indicator overlay */}
          {isAdded && (
            <div className="absolute top-2 left-2">
              <Badge
                variant="outline"
                className="border-success/50 bg-success/10 text-success backdrop-blur-sm"
              >
                Added
              </Badge>
            </div>
          )}
        </div>

        {/* Content area */}
        <div className="p-4 flex flex-col flex-1">
          {/* Header row with name and tool badge */}
          <div className="flex items-start justify-between gap-2 mb-2">
            <div className="min-w-0 flex-1">
              <Type
                variant="subheading"
                as="div"
                className="truncate text-md group-hover:text-primary transition-colors"
                title={displayName}
              >
                {displayName}
              </Type>
              <Type small muted className="truncate">
                v{server.version}
              </Type>
            </div>
            <ToolCollectionBadge toolNames={toolNames} />
          </div>

          {/* Description */}
          <Type small muted className="line-clamp-2 mb-3">
            {server.description}
          </Type>

          {/* Footer row with stats and action */}
          <div className="flex items-center justify-between gap-2 mt-auto pt-2">
            {/* Usage stats */}
            <div className="flex items-center gap-2">
              {metadata.visitorsMonth > 0 ? (
                <Type small muted>
                  {metadata.visitorsMonth.toLocaleString()} monthly users
                </Type>
              ) : (
                <Badge variant="outline" className="text-xs">
                  New
                </Badge>
              )}
            </div>

            {/* Action button */}
            {isAdded ? (
              <Button
                variant="secondary"
                size="sm"
                disabled={isRemoving}
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  if (existingMcp) {
                    onRemove(existingMcp.slug);
                  }
                }}
              >
                <Button.LeftIcon>
                  {isRemoving ? (
                    <Loader2 className="w-3.5 h-3.5 animate-spin" />
                  ) : (
                    <Minus className="w-3.5 h-3.5" />
                  )}
                </Button.LeftIcon>
                <Button.Text>
                  {isRemoving ? "Removing..." : "Remove"}
                </Button.Text>
              </Button>
            ) : (
              <Button
                variant="secondary"
                size="sm"
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  onAdd();
                }}
              >
                <Button.LeftIcon>
                  <Plus className="w-3.5 h-3.5" />
                </Button.LeftIcon>
                <Button.Text>Add</Button.Text>
              </Button>
            )}
          </div>
        </div>
      </div>
    </Link>
  );
}
