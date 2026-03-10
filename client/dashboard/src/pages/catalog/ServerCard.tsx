import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import type { DeploymentExternalMCP } from "@gram/client/models/components";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { ArrowRight, Check } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router";
import type { Server } from "./hooks";

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
  const displayName = server.title ?? server.registrySpecifier;

  const existingMcp = externalMcps.find(
    (mcp) => mcp.registryServerSpecifier === server.registrySpecifier,
  );
  const isAdded = !!existingMcp;

  // Get tool names for the badge tooltip
  const toolNames = useMemo(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const serverTools = (server.tools ?? []) as any[];
    return serverTools.map((t) => t.name || "Unknown tool");
  }, [server.tools]);

  // Check server hosting/auth status
  const isSelfHosted = server.isSelfHosted ?? false;
  const requiresAuth = server.requiresAuth ?? false;

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
      className={cn(
        "server-card group bg-card text-card-foreground flex flex-row rounded-xl border !border-foreground/10 overflow-hidden",
        "hover:!border-foreground/30 hover:shadow-md transition-all cursor-pointer h-full",
        isAdded && "border-success/50 ring-1 ring-success/20",
      )}
    >
      {/* Illustration sidebar with dot pattern */}
      <div className="w-40 shrink-0 overflow-hidden border-r relative bg-muted/30 text-muted-foreground/20">
        <div
          className="absolute inset-0 scroll-dots-target"
          style={{
            backgroundImage:
              "radial-gradient(circle, currentColor 1px, transparent 1px)",
            backgroundSize: "16px 16px",
          }}
        />
        {/* Logo */}
        {server.iconUrl && (
          <div className="absolute inset-0 flex items-center justify-center">
            <div className="bg-background/90 backdrop-blur-sm rounded-lg p-3 shadow-lg">
              <img
                src={server.iconUrl}
                alt={displayName}
                className="w-12 h-12 object-contain"
              />
            </div>
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
          {/* Tool badge: hide for self-hosted, show "Needs auth" for OAuth servers */}
          {toolNames.length === 0 && isSelfHosted ? null : toolNames.length ===
              0 && requiresAuth ? (
            <Badge variant="warning">
              <Badge.Text>Needs OAuth</Badge.Text>
            </Badge>
          ) : (
            <ToolCollectionBadge toolNames={toolNames} />
          )}
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
      </div>
    </div>
  );
}
