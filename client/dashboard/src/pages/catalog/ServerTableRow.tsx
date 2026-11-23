import { resolveCatalogLogo } from "./logo";
import { useIsDarkTheme } from "@/lib/theme";
import { CatalogLogo } from "./CatalogLogo";
import { DotRow } from "@/components/ui/DotRow";
import { Text } from "@/components/ui/Text";
import { Badge } from "@/components/ui/Badge";
import type { CatalogInstallState } from "./catalogInstall";
import type { CatalogServer } from "./hooks";
import { ManualSetupBadge } from "./ManualSetupBadge";
import { CatalogInstallButton, TeamUsageNote } from "./ServerCard";
import type { CatalogTeamUsage } from "./team-usage";

interface ServerTableRowProps {
  server: CatalogServer;
  detailHref: string;
  /** Unknown while install lookups are loading or failed, so the row does not flash Add. */
  installState: CatalogInstallState;
  /** Starts the install flow for this one server. */
  onAdd: () => void;
  /** Whether the table has a "Your team" column (org admins only). */
  showTeamUsage?: boolean;
  /** Absent while it loads and for servers nobody uses. */
  teamUsage?: CatalogTeamUsage;
}

export function ServerTableRow({
  server,
  detailHref,
  installState,
  onAdd,
  showTeamUsage = false,
  teamUsage,
}: ServerTableRowProps): JSX.Element {
  const displayName = server.title ?? server.registrySpecifier;
  const theme = useIsDarkTheme() ? "dark" : "light";

  return (
    <DotRow
      href={detailHref}
      ariaLabel={`View ${displayName}`}
      iconRailClassName="bg-surface-secondary-default"
      iconTileClassName="bg-card dark:bg-card"
      icon={
        resolveCatalogLogo(server, theme).src ? (
          <CatalogLogo
            server={server}
            alt={displayName}
            className="h-6 w-6 object-contain"
          />
        ) : undefined
      }
    >
      {/* Name */}
      <td className="px-3 py-3">
        <div className="flex items-center gap-2">
          <Text
            variant="subheading"
            as="div"
            className="group-hover:text-primary truncate text-sm transition-colors"
            title={displayName}
          >
            {displayName}
          </Text>
          {installState === "added" && (
            <Badge variant="success">
              <Badge.Text>Added</Badge.Text>
            </Badge>
          )}
          <ManualSetupBadge server={server} />
        </div>
      </td>

      {/* Version */}
      <td className="px-3 py-3">
        <Text small muted>
          v{server.version}
        </Text>
      </td>

      {/* Description */}
      <td className="max-w-xs px-3 py-3">
        <Text small muted className="block truncate">
          {server.description}
        </Text>
      </td>

      {/* The team's own use of the server; fills in after the rows load. */}
      {showTeamUsage && (
        <td className="px-3 py-3">
          {/* Only the note sits above the row's link, for its tooltip; the
              rest of the cell still opens the server. */}
          {teamUsage && (
            <span className="relative z-20 inline-flex max-w-full">
              <TeamUsageNote usage={teamUsage} />
            </span>
          )}
        </td>
      )}

      {/* Add — sits above the row's stretched link overlay so it stays
          clickable without navigating. */}
      <td className="relative z-20 px-3 py-3">
        <CatalogInstallButton
          state={installState}
          onAdd={onAdd}
          iconClassName="h-3.5 w-3.5"
          stopNavigation
        />
      </td>
    </DotRow>
  );
}
