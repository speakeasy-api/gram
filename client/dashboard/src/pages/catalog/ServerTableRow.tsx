import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { DotRow } from "@/components/ui/DotRow";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { ArrowRight, Check } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router";
import type { PulseMCPServer } from "./hooks";
import { parseServerMetadata } from "./hooks/serverMetadata";
import { ManualSetupBadge } from "./ManualSetupBadge";

interface ServerTableRowProps {
  server: PulseMCPServer;
  detailHref: string;
  /** Whether this catalog server is already installed in the project. */
  isAdded: boolean;
  isSelected?: boolean;
  onToggleSelect?: () => void;
}

export function ServerTableRow({
  server,
  detailHref,
  isAdded,
  isSelected,
  onToggleSelect,
}: ServerTableRowProps): JSX.Element {
  const metadata = useMemo(() => parseServerMetadata(server), [server]);
  const displayName = server.title ?? server.registrySpecifier;

  // The catalog list carries a precomputed tool count, not the tool defs.
  const toolCount = server.toolCount;

  // Remote-only servers (auth-gated proxies like GitHub, Make) can't enumerate
  // tools until a user authenticates, so the "No Tools" badge would be
  // misleading. Hide it for them.
  const isRemoteOnly = (server.remotes?.length ?? 0) > 0 && toolCount === 0;

  const handleRowClick = (e: React.MouseEvent<HTMLTableRowElement>) => {
    e.stopPropagation();
    onToggleSelect?.();
  };

  return (
    <DotRow
      onClick={handleRowClick}
      className={cn(isAdded && "border-l-success/50 border-l-2")}
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
      {/* Selection */}
      <td className="w-10 px-3 py-3">
        {isSelected ? (
          <div className="bg-foreground flex size-4 items-center justify-center">
            <Check className="text-background size-3" strokeWidth={3} />
          </div>
        ) : (
          <div className="border-border size-4 border" />
        )}
      </td>

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

      {/* View */}
      <td className="px-3 py-3">
        <Link to={detailHref} onClick={(e) => e.stopPropagation()}>
          <Button variant="secondary" size="sm">
            <Button.Text>View</Button.Text>
            <Button.RightIcon>
              <ArrowRight className="h-3.5 w-3.5" />
            </Button.RightIcon>
          </Button>
        </Link>
      </td>
    </DotRow>
  );
}
