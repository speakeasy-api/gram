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
import { ArrowRight, Check } from "lucide-react";
import { useMemo, useState } from "react";
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

  const [isHovered, setIsHovered] = useState(false);

  const handleCardClick = (e: React.MouseEvent<HTMLDivElement>) => {
    e.stopPropagation(); // Prevent click-outside-to-deselect from firing
    // Reset hover state when deselecting so image goes back to desaturated
    if (isSelected) {
      setIsHovered(false);
    }
    onToggleSelect?.();
  };

  return (
    // biome-ignore lint/a11y/useSemanticElements: Card contains nested interactive elements (buttons, links)
    <div
      role="button"
      tabIndex={0}
      onClick={handleCardClick}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.stopPropagation();
          onToggleSelect?.();
        }
      }}
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      className={cn(
        "group bg-card text-card-foreground flex flex-col rounded-xl border overflow-hidden",
        "hover:border-foreground/20 hover:shadow-md transition-all cursor-pointer h-full",
        isAdded && "border-success/50 ring-1 ring-success/20",
      )}
    >
      {/* Illustration header */}
      <div className="h-36 w-full overflow-hidden border-b relative">
        {server.iconUrl ? (
          <ExternalMCPIllustration
            slug={slug}
            logoUrl={server.iconUrl}
            name={displayName}
            className={isSelected || isHovered ? "saturate-100" : undefined}
          />
        ) : (
          <MCPPatternIllustration
            toolsetSlug={slug}
            className={cn(
              "transition-all duration-300",
              isSelected || isHovered ? "saturate-100" : "saturate-[.3]",
            )}
          />
        )}
        {/* Badge overlays - top right */}
        <div className="absolute top-3.5 right-3.5 flex flex-col gap-1.5 items-end">
          {metadata.isOfficial && (
            <Badge
              variant="outline"
              className="bg-white/70 text-black backdrop-blur-sm border-white/50"
            >
              Official
            </Badge>
          )}
          {metadata.visitorsMonth === 0 && (
            <Badge
              variant="outline"
              className="bg-white/70 text-black backdrop-blur-sm border-white/50"
            >
              New
            </Badge>
          )}
        </div>
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
          {/* Selection indicator */}
          {isSelected ? (
            <div className="size-7 rounded-full border-[1.5px] border-[#1DA1F2] flex items-center justify-center">
              <Check className="size-4 text-[#1DA1F2]" strokeWidth={3} />
            </div>
          ) : (
            <div className="size-7 rounded-full border-[1.5px] border-muted-foreground/30" />
          )}

          {/* View Details button */}
          <Link
            to={detailHref}
            onClick={(e) => e.stopPropagation()}
            className="ml-auto"
          >
            <Button variant="secondary" size="sm">
              <Button.Text>View Details</Button.Text>
              <ArrowRight className="w-4 h-4" />
            </Button>
          </Link>
        </div>
      </div>
    </div>
  );
}
