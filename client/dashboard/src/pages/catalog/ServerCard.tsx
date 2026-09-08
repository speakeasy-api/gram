import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { Card } from "@/components/ui/Card";
import { useIconConfetti } from "@/components/icon-confetti";
import { Text } from "@/components/ui/Text";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Plus } from "lucide-react";
import { Link } from "react-router";
import type { PulseMCPServer } from "./hooks";
import { ManualSetupBadge } from "./ManualSetupBadge";

interface ServerCardProps {
  server: PulseMCPServer;
  detailHref: string;
  /** Whether this catalog server is already installed in the project. */
  isAdded: boolean;
  /** Starts the install flow for this one server. */
  onAdd: () => void;
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
  isAdded,
  onAdd,
}: ServerCardProps): JSX.Element {
  const displayName = server.title ?? server.registrySpecifier;
  const { canvasRef, start, stop } = useIconConfetti();

  // The catalog list carries a precomputed tool count, not the tool defs.
  const toolCount = server.toolCount;

  // Remote-only servers (auth-gated proxies like GitHub, Make) can't enumerate
  // tools until a user authenticates, so the "No Tools" badge would be
  // misleading. Hide it for them.
  const isRemoteOnly = (server.remotes?.length ?? 0) > 0 && toolCount === 0;

  return (
    // The whole card is the link to the detail page; Add is the one control
    // that opts out of it.
    <Link
      to={detailHref}
      onMouseEnter={start}
      onMouseLeave={stop}
      className="focus-visible:ring-ring block h-full no-underline hover:no-underline focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
    >
      <Card.Entity
        className="cursor-pointer"
        icon={
          server.iconUrl ? (
            <img
              src={server.iconUrl}
              alt={displayName}
              className="h-12 w-12 object-contain"
            />
          ) : undefined
        }
        iconRailClassName="isolate"
        iconTileClassName="icon-hover-pulse"
        overlay={
          <>
            <canvas
              ref={canvasRef}
              aria-hidden="true"
              className="pointer-events-none absolute inset-0 -z-10 size-full"
            />
            {isAdded && (
              <div className="absolute top-3.5 left-3.5 z-10">
                <Badge variant="success">
                  <Badge.Text>Added</Badge.Text>
                </Badge>
              </div>
            )}
          </>
        }
      >
        {/* Header row with name and tool badge */}
        <div className="mb-2 flex items-start justify-between gap-2">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <Text
                variant="subheading"
                as="div"
                className="text-md group-hover:text-primary truncate transition-colors"
                title={displayName}
              >
                {displayName}
              </Text>
            </div>
            <Text small muted className="truncate">
              v{server.version}
            </Text>
          </div>
          <div className="flex items-baseline gap-1">
            <ManualSetupBadge server={server} className="mr-1" />
            <ToolCollectionBadge
              count={toolCount}
              emptyLabel={isRemoteOnly ? null : undefined}
            />
          </div>
        </div>

        {/* Description */}
        <Text small muted className="mb-3 line-clamp-2">
          {server.description}
        </Text>

        {/* Footer row with the install action */}
        <div className="mt-auto flex items-center justify-end gap-2 pt-2">
          <Button
            variant={isAdded ? "secondary" : "primary"}
            size="sm"
            onClick={(e) => {
              // The card is a link; adding must not also navigate.
              e.preventDefault();
              e.stopPropagation();
              onAdd();
            }}
          >
            <Button.LeftIcon>
              <Plus className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>{isAdded ? "Add again" : "Add"}</Button.Text>
          </Button>
        </div>
      </Card.Entity>
    </Link>
  );
}
