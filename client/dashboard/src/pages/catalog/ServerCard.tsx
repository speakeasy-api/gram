import { Badge } from "@/components/ui/badge";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import type { DeploymentExternalMCP } from "@gram/client/models/components";
import { Button, Stack } from "@speakeasy-api/moonshine";
import {
  Key,
  Loader2,
  Lock,
  Minus,
  Plus,
  Server as ServerIcon,
  Shield,
  Wrench,
} from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router";
import type { Server } from "./hooks";
import { parseServerMetadata } from "./hooks/serverMetadata";
import { Sparkline, SparklinePlaceholder } from "./Sparkline";

interface ServerCardProps {
  server: Server;
  detailHref: string;
  externalMcps: DeploymentExternalMCP[];
  onAdd: () => void;
  onRemove: (slug: string) => void;
  isRemoving: boolean;
}

/**
 * Icon component for auth type display.
 */
function AuthIcon({ authType }: { authType: string }) {
  switch (authType) {
    case "none":
      return <Lock className="w-3 h-3" />;
    case "apikey":
      return <Key className="w-3 h-3" />;
    case "oauth":
      return <Shield className="w-3 h-3" />;
    default:
      return <Key className="w-3 h-3" />;
  }
}

/**
 * Badge showing auth type with icon.
 */
function AuthBadge({
  authType,
  authTypeDisplay,
}: {
  authType: string;
  authTypeDisplay: string;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs",
        authType === "none"
          ? "bg-success/10 text-success"
          : "bg-muted text-muted-foreground",
      )}
    >
      <AuthIcon authType={authType} />
      <span>{authTypeDisplay}</span>
    </span>
  );
}

/**
 * Badge showing tool count.
 */
function ToolCountBadge({ count }: { count: number }) {
  return (
    <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs bg-muted text-muted-foreground">
      <Wrench className="w-3 h-3" />
      <span>
        {count} {count === 1 ? "tool" : "tools"}
      </span>
    </span>
  );
}

/**
 * Official badge with gold/amber accent.
 */
function OfficialBadge() {
  return (
    <Badge
      variant="outline"
      className="border-warning/50 bg-warning/10 text-warning"
    >
      Official
    </Badge>
  );
}

/**
 * Read-only safety badge.
 */
function SafetyBadge() {
  return (
    <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-xs bg-success/10 text-success">
      <Shield className="w-3 h-3" />
      <span>Read-only</span>
    </span>
  );
}

/**
 * Enhanced server card with rich metadata display.
 *
 * Features:
 * - Tool count badge
 * - Auth type badge with icon
 * - Sparkline showing usage trend
 * - Official badge (gold/amber)
 * - Hover effects for interactivity
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

  return (
    <Link to={detailHref}>
      <div
        className={cn(
          "group flex flex-col gap-3 rounded-xl border bg-card p-5",
          "hover:border-primary/50 hover:shadow-md transition-all h-full",
          isAdded && "border-success/30 bg-success/5",
        )}
      >
        {/* Header row: Icon + Title + Official badge */}
        <Stack direction="horizontal" gap={3}>
          <div
            className={cn(
              "w-12 h-12 rounded-lg flex items-center justify-center shrink-0 transition-colors",
              "bg-primary/10 group-hover:bg-primary/15",
            )}
          >
            {server.iconUrl ? (
              <img
                src={server.iconUrl}
                alt={displayName}
                className="w-8 h-8 rounded"
              />
            ) : (
              <ServerIcon className="w-6 h-6 text-muted-foreground" />
            )}
          </div>
          <Stack gap={1} className="min-w-0 flex-1">
            <Stack
              direction="horizontal"
              gap={2}
              align="center"
              className="flex-wrap"
            >
              <Type
                variant="subheading"
                className="group-hover:text-primary transition-colors truncate"
              >
                {displayName}
              </Type>
              {metadata.isOfficial && <OfficialBadge />}
              {isAdded && (
                <Badge
                  variant="outline"
                  className="border-success/50 text-success"
                >
                  Added
                </Badge>
              )}
            </Stack>
            <Type small muted className="truncate">
              {server.registrySpecifier} • v{server.version}
            </Type>
          </Stack>
        </Stack>

        {/* Description */}
        <Type small muted className="line-clamp-2">
          {server.description}
        </Type>

        {/* Metadata badges row */}
        <Stack direction="horizontal" gap={2} className="flex-wrap">
          <ToolCountBadge count={metadata.toolCount} />
          <AuthBadge
            authType={metadata.authType}
            authTypeDisplay={metadata.authTypeDisplay}
          />
          {metadata.isReadOnly && metadata.toolCount > 0 && <SafetyBadge />}
        </Stack>

        {/* Footer: Usage stats + Action button */}
        <div className="mt-auto pt-2">
          <Stack direction="horizontal" justify="space-between" align="center">
            <Stack direction="horizontal" gap={2} align="center">
              {metadata.visitorsMonth > 0 ? (
                <>
                  <Sparkline
                    data={metadata.weeklyData}
                    height={14}
                    barWidth={3}
                    gap={1}
                  />
                  <Type small muted>
                    {metadata.visitorsMonth.toLocaleString()} monthly
                  </Type>
                </>
              ) : (
                <SparklinePlaceholder />
              )}
            </Stack>

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
          </Stack>
        </div>
      </div>
    </Link>
  );
}
