import { catalogLogoClassName } from "./logo";
import { cn } from "@/lib/utils";
import { DotRow } from "@/components/ui/DotRow";
import { Text } from "@/components/ui/Text";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Plus } from "lucide-react";
import type { CatalogServer } from "./hooks";
import { ManualSetupBadge } from "./ManualSetupBadge";

interface ServerTableRowProps {
  server: CatalogServer;
  detailHref: string;
  /** Whether this catalog server is already installed in the project. */
  isAdded: boolean;
  /** Starts the install flow for this one server. */
  onAdd: () => void;
}

export function ServerTableRow({
  server,
  detailHref,
  isAdded,
  onAdd,
}: ServerTableRowProps): JSX.Element {
  const displayName = server.title ?? server.registrySpecifier;

  return (
    <DotRow
      href={detailHref}
      ariaLabel={`View ${displayName}`}
      iconRailClassName="bg-surface-secondary-default"
      iconTileClassName="bg-card dark:bg-card"
      icon={
        server.iconUrl ? (
          <img
            src={server.iconUrl}
            alt={displayName}
            className={cn(
              "h-6 w-6 object-contain",
              catalogLogoClassName(server.registrySpecifier),
            )}
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
          {isAdded && (
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

      {/* Add — sits above the row's stretched link overlay so it stays
          clickable without navigating. */}
      <td className="relative z-20 px-3 py-3">
        <Button
          variant={isAdded ? "secondary" : "primary"}
          size="sm"
          onClick={(e) => {
            e.preventDefault();
            e.stopPropagation();
            onAdd();
          }}
        >
          <Button.LeftIcon>
            <Plus className="h-3.5 w-3.5" />
          </Button.LeftIcon>
          <Button.Text>{isAdded ? "Add again" : "Add"}</Button.Text>
        </Button>
      </td>
    </DotRow>
  );
}
