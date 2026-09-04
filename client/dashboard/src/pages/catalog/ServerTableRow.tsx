import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { DotRow } from "@/components/ui/DotRow";
import { Text } from "@/components/ui/Text";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Plus } from "lucide-react";
import { useMemo } from "react";
import type { PulseMCPServer } from "./hooks";
import { parseServerMetadata } from "./hooks/serverMetadata";
import { ManualSetupBadge } from "./ManualSetupBadge";

interface ServerTableRowProps {
  server: PulseMCPServer;
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
  const metadata = useMemo(() => parseServerMetadata(server), [server]);
  const displayName = server.title ?? server.registrySpecifier;

  // The catalog list carries a precomputed tool count, not the tool defs.
  const toolCount = server.toolCount;

  // Remote-only servers (auth-gated proxies like GitHub, Make) can't enumerate
  // tools until a user authenticates, so the "No Tools" badge would be
  // misleading. Hide it for them.
  const isRemoteOnly = (server.remotes?.length ?? 0) > 0 && toolCount === 0;

  return (
    <DotRow
      href={detailHref}
      ariaLabel={`View ${displayName}`}
      icon={
        server.iconUrl ? (
          <img
            src={server.iconUrl}
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
          {isAdded && (
            <Badge variant="success">
              <Badge.Text>Added</Badge.Text>
            </Badge>
          )}
          {metadata.visitorsMonth === 0 && (
            <Badge variant="neutral">
              <Badge.Text>New</Badge.Text>
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

      {/* Tools */}
      <td className="px-3 py-3">
        <ToolCollectionBadge
          count={toolCount}
          emptyLabel={isRemoteOnly ? null : undefined}
        />
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
