import { ToolCollectionBadge } from "@/components/tool-collection-badge";
import { Badge } from "@/components/ui/badge";
import { DotRow } from "@/components/ui/dot-row";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import type { DeploymentExternalMCP } from "@gram/client/models/components";
import { Button } from "@speakeasy-api/moonshine";
import { ArrowRight, Check } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router";
import type { Server } from "./hooks";
import { parseServerMetadata } from "./hooks/serverMetadata";

interface ServerTableRowProps {
  server: Server;
  detailHref: string;
  externalMcps: DeploymentExternalMCP[];
  isSelected?: boolean;
  onToggleSelect?: () => void;
}

export function ServerTableRow({
  server,
  detailHref,
  externalMcps,
  isSelected,
  onToggleSelect,
}: ServerTableRowProps) {
  const metadata = useMemo(() => parseServerMetadata(server), [server]);
  const displayName = server.title ?? server.registrySpecifier;

  const existingMcp = externalMcps.find(
    (mcp) => mcp.registryServerSpecifier === server.registrySpecifier,
  );
  const isAdded = !!existingMcp;

  const toolNames = useMemo(() => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const tools = (server.tools ?? []) as any[];
    return tools.map((t) => t.name || "Unknown tool");
  }, [server.tools]);

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
          <div className="flex size-5 items-center justify-center rounded-full bg-[#1DA1F2]">
            <Check className="size-3 text-white" strokeWidth={5} />
          </div>
        ) : (
          <div className="border-muted-foreground/30 size-5 rounded-full border-2" />
        )}
      </td>

      {/* Name */}
      <td className="px-3 py-3">
        <div className="flex items-center gap-2">
          <Type
            variant="subheading"
            as="div"
            className="group-hover:text-primary truncate text-sm transition-colors"
            title={displayName}
          >
            {displayName}
          </Type>
          {isAdded && (
            <Badge
              variant="outline"
              className="border-success/50 bg-success/10 text-success text-xs"
            >
              Added
            </Badge>
          )}
          {metadata.visitorsMonth === 0 && (
            <Badge variant="outline" className="text-xs">
              New
            </Badge>
          )}
        </div>
      </td>

      {/* Version */}
      <td className="px-3 py-3">
        <Type small muted>
          v{server.version}
        </Type>
      </td>

      {/* Description */}
      <td className="max-w-xs px-3 py-3">
        <Type small muted className="block truncate">
          {server.description}
        </Type>
      </td>

      {/* Tools */}
      <td className="px-3 py-3">
        <ToolCollectionBadge toolNames={toolNames} />
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
