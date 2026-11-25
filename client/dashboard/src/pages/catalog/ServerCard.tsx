import { resolveCatalogLogo } from "./logo";
import { useIsDarkTheme } from "@/lib/theme";
import { CatalogLogo } from "./CatalogLogo";
import { Card } from "@/components/ui/Card";
import { useIconDither } from "@/components/icon-dither";
import { Text } from "@/components/ui/Text";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Plus, Users } from "lucide-react";
import { Link } from "react-router";
import type { CatalogInstallState } from "./catalogInstall";
import type { CatalogServer } from "./hooks";
import { ManualSetupBadge } from "./ManualSetupBadge";
import {
  TEAM_USAGE_TOOLTIP,
  teamUsageLabel,
  type CatalogTeamUsage,
} from "./team-usage";

interface ServerCardProps {
  server: CatalogServer;
  detailHref: string;
  /** Unknown while install lookups are loading or failed, so the card does not flash Add. */
  installState: CatalogInstallState;
  /** Starts the install flow for this one server. */
  onAdd: () => void;
  /**
   * How much the team already uses this server directly, from Shadow MCP.
   * Absent while it loads, for servers nobody uses, and for non-admins.
   */
  teamUsage?: CatalogTeamUsage;
}

export function CatalogInstallButton({
  state,
  onAdd,
  size = "sm",
  iconClassName = "h-4 w-4",
  stopNavigation = false,
}: {
  state: CatalogInstallState;
  onAdd: () => void;
  size?: "sm" | "md";
  iconClassName?: string;
  /** The catalog card and row are links; the button must not follow them. */
  stopNavigation?: boolean;
}): JSX.Element {
  if (state === "unknown") {
    return (
      <Skeleton
        className={size === "md" ? "h-10 w-28" : "h-8 w-24"}
        aria-hidden
      />
    );
  }

  const added = state === "added";
  return (
    <Button
      variant={added ? "secondary" : "primary"}
      size={size}
      onClick={(event) => {
        if (stopNavigation) {
          event.preventDefault();
          event.stopPropagation();
        }
        onAdd();
      }}
    >
      <Button.LeftIcon>
        <Plus className={iconClassName} />
      </Button.LeftIcon>
      <Button.Text>{added ? "Add again" : "Add"}</Button.Text>
    </Button>
  );
}

/**
 * Catalog server card matching the MCPServerCard design pattern.
 */
export function ServerCard({
  server,
  detailHref,
  installState,
  onAdd,
  teamUsage,
}: ServerCardProps): JSX.Element {
  const displayName = server.title ?? server.registrySpecifier;
  const theme = useIsDarkTheme() ? "dark" : "light";
  const { canvasRef, start, stop } = useIconDither();

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
          resolveCatalogLogo(server, theme).src ? (
            <CatalogLogo
              server={server}
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
            {installState === "added" && (
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
          </div>
        </div>

        {/* Description */}
        <Text small muted className="mb-3 line-clamp-2">
          {server.description}
        </Text>

        {/* Footer row: the team's own use of the server, then the install
            action. The usage fills in after the cards load, in a row that is
            already there, so nothing moves. */}
        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          {teamUsage ? <TeamUsageNote usage={teamUsage} /> : <span />}
          <CatalogInstallButton
            state={installState}
            onAdd={onAdd}
            stopNavigation
          />
        </div>
      </Card.Entity>
    </Link>
  );
}

/** "Used by 5 people · 51 calls", with where the counts come from on hover. */
export function TeamUsageNote({
  usage,
}: {
  usage: CatalogTeamUsage;
}): JSX.Element {
  return (
    <SimpleTooltip tooltip={TEAM_USAGE_TOOLTIP}>
      <span
        tabIndex={0}
        className="text-muted-foreground flex min-w-0 items-center gap-1.5 text-xs"
      >
        <Users className="h-3.5 w-3.5 shrink-0" aria-hidden />
        <span className="truncate">{teamUsageLabel(usage)}</span>
      </span>
    </SimpleTooltip>
  );
}
