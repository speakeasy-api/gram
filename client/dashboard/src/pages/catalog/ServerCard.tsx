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
import { useMemo } from "react";
import { Link } from "react-router";
import type { Server } from "./hooks";
import { parseServerMetadata } from "./hooks/serverMetadata";

interface ServerCardProps {
  server: Server;
  detailHref: string;
  externalMcps: DeploymentExternalMCP[];
  isSelected?: boolean;
  onToggleSelect?: () => void;
}

/**
 * Server card matching the MCPCard design pattern.
 *
 * Features:
 * - Pattern illustration header with logo overlay
 * - Tool count badge
 * - Official badge
 * - Monthly users count
 */
export function ServerCard({
  server,
  detailHref,
  externalMcps,
  isSelected,
  onToggleSelect,
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

  const handleCardClick = () => {
    if (onToggleSelect) {
      onToggleSelect();
    }
  };

  return (
    // biome-ignore lint/a11y/useSemanticElements: Card contains nested interactive elements (buttons, links)
    <div
      role="button"
      tabIndex={0}
      onClick={handleCardClick}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          handleCardClick();
        }
      }}
      className={cn(
        "group bg-card text-card-foreground flex flex-col rounded-xl border overflow-hidden",
        "hover:border-foreground/20 hover:shadow-md transition-all cursor-pointer h-full",
        isAdded && "border-success/50 ring-1 ring-success/20",
        isSelected && "border-primary ring-2 ring-primary",
      )}
    >
      {/* Illustration header */}
      <div className="h-36 w-full overflow-hidden border-b relative">
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
              className="bg-background/50 text-foreground backdrop-blur-sm"
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

        {/* Footer row with stats and actions */}
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

          {/* View Details button */}
          <Link to={detailHref} onClick={(e) => e.stopPropagation()}>
            <Button variant="secondary" size="sm">
              <Button.Text>View Details</Button.Text>
            </Button>
          </Link>
        </div>
      </div>
    </div>
  );
}
