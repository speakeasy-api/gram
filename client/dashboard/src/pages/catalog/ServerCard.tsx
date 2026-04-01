import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { Badge } from "@/components/ui/badge";
import { DotCard } from "@/components/ui/dot-card";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import type { DeploymentExternalMCP } from "@gram/client/models/components";
import { Button } from "@speakeasy-api/moonshine";
import { ArrowRight, Check } from "lucide-react";
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

  // Get tool names for the badge tooltip
  const toolNames = useMemo(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const tools = (server.tools ?? []) as any[];
    return tools.map((t) => t.name || "Unknown tool");
  }, [server.tools]);

  const handleCardClick = (e: React.MouseEvent<HTMLDivElement>) => {
    e.stopPropagation(); // Prevent click-outside-to-deselect from firing
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
    >
      <DotCard
        className={cn(
          "cursor-pointer",
          isAdded && "border-success/50 ring-1 ring-success/20",
        )}
        icon={
          server.iconUrl ? (
            <img
              src={server.iconUrl}
              alt={displayName}
              className="w-12 h-12 object-contain"
            />
          ) : undefined
        }
        overlay={
          isAdded ? (
            <div className="absolute top-3.5 left-3.5 z-10">
              <Badge
                variant="outline"
                className="border-success/50 bg-success/10 text-success backdrop-blur-sm"
              >
                Added
              </Badge>
            </div>
          ) : undefined
        }
      >
        {/* Header row with name and tool badge */}
        <div className="flex items-start justify-between gap-2 mb-2">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <Type
                variant="subheading"
                as="div"
                className="truncate text-md group-hover:text-primary transition-colors"
                title={displayName}
              >
                {displayName}
              </Type>
              {metadata.visitorsMonth === 0 && (
                <Badge variant="outline" className="shrink-0">
                  New
                </Badge>
              )}
            </div>
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
            <div className="size-6 rounded-full bg-[#1DA1F2] flex items-center justify-center">
              <Check className="size-3.5 text-white" strokeWidth={5} />
            </div>
          ) : (
            <div className="size-6 rounded-full border-2 border-muted-foreground/30" />
          )}

          {/* View Details button */}
          <Link
            to={detailHref}
            onClick={(e) => e.stopPropagation()}
            className="ml-auto"
          >
            <Button variant="secondary" size="sm">
              <Button.Text>View</Button.Text>
              <Button.RightIcon>
                <ArrowRight className="w-4 h-4" />
              </Button.RightIcon>
            </Button>
          </Link>
        </div>
      </DotCard>
    </div>
  );
}
