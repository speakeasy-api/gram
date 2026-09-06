import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { DotRow } from "@/components/ui/DotRow";
import { MoreActions } from "@/components/ui/MoreActions";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import {
  formatRemoteMcpDisplay,
  formatTunneledMcpDisplay,
  sourceTypeToUrnKind,
} from "@/lib/sources";
import { useRoutes } from "@/routes";
import { Badge } from "@/components/ui/Badge";
import { CircleAlertIcon, FileCode, Network } from "lucide-react";
import { SourceMcpIcon, type NamedAsset } from "./SourceCard";

const sourceTypeConfig = {
  openapi: { label: "OpenAPI" },
  function: { label: "Function" },
  externalmcp: { label: "Catalog" },
  remotemcp: { label: "Remote MCP" },
  tunneledmcp: { label: "Tunneled MCP" },
  unproxiedmcp: { label: "Unproxied MCP" },
};

// sourceRowDisplayName mirrors [sourceCardNameAndSubtitle] in SourceCard.tsx:
// a flat switch instead of a nested ternary for source types whose display
// name falls back to a URL when unnamed.
function sourceRowDisplayName(asset: NamedAsset): string | undefined {
  switch (asset.type) {
    case "remotemcp":
    case "unproxiedmcp":
      return formatRemoteMcpDisplay(asset);
    case "tunneledmcp":
      return formatTunneledMcpDisplay(asset);
    case "openapi":
    case "function":
    case "externalmcp":
      return asset.name;
  }
}

function formatDate(date: Date | undefined) {
  if (!date) return "—";
  return date.toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

export function SourceTableRow({
  asset,
  causingFailure,
  toolCount,
  deploymentId,
  handleRemove,
  handleViewAsset,
  setChangeDocumentTargetSlug,
}: {
  asset: NamedAsset;
  causingFailure?: boolean;
  toolCount: number;
  deploymentId?: string;
  handleRemove: (assetId: string) => void;
  handleViewAsset: (assetId: string) => void;
  setChangeDocumentTargetSlug: (slug: string) => void;
}): JSX.Element {
  const routes = useRoutes();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("project:write");
  const config = sourceTypeConfig[asset.type];
  const sourceTypeLabel = config.label;
  const sourceKind = sourceTypeToUrnKind(asset.type);

  const createdAt = "createdAt" in asset ? asset.createdAt : undefined;
  const updatedAt = "updatedAt" in asset ? asset.updatedAt : undefined;

  const actions =
    asset.type === "remotemcp" ||
    asset.type === "tunneledmcp" ||
    asset.type === "unproxiedmcp"
      ? []
      : [
          ...(asset.type === "openapi"
            ? [
                {
                  label: "View",
                  onClick: () => handleViewAsset(asset.id),
                  icon: "eye" as const,
                },
                {
                  label: "Update",
                  onClick: () => setChangeDocumentTargetSlug(asset.slug),
                  icon: "upload" as const,
                  disabled: !canWrite,
                },
              ]
            : []),
          ...(deploymentId
            ? [
                {
                  label: "Deployment",
                  onClick: () =>
                    routes.deployments.deployment.goTo(deploymentId),
                  icon: "history" as const,
                },
              ]
            : []),
          {
            label: "Delete",
            onClick: () => handleRemove(asset.id),
            icon: "trash" as const,
            destructive: true,
            disabled: !canWrite,
          },
        ];

  const iconContent = (() => {
    if (asset.type === "externalmcp" && asset.iconUrl) {
      return (
        <img
          src={asset.iconUrl}
          alt={asset.name}
          className="h-6 w-6 object-contain"
        />
      );
    }
    if (
      asset.type === "remotemcp" ||
      asset.type === "tunneledmcp" ||
      asset.type === "unproxiedmcp"
    ) {
      return (
        <SourceMcpIcon
          mcpServerId={asset.mcpServerId}
          className="h-5 w-5 object-contain"
        />
      );
    }
    if (asset.type === "externalmcp") {
      return <Network className="text-muted-foreground h-5 w-5" />;
    }
    return <FileCode className="text-muted-foreground h-5 w-5" />;
  })();

  const displayName = sourceRowDisplayName(asset);

  return (
    <TableRowContextMenu actions={actions}>
      <DotRow
        icon={iconContent}
        href={routes.sources.source.href(sourceKind, asset.slug)}
        ariaLabel={`View source ${displayName}`}
      >
        {/* Name */}
        <td className="px-3 py-3">
          <Text
            variant="subheading"
            as="div"
            className="group-hover:text-primary truncate text-sm transition-colors"
            title={displayName}
          >
            {displayName}
          </Text>
        </td>

        {/* Type */}
        <td className="px-3 py-3">
          <Badge variant="neutral">{sourceTypeLabel}</Badge>
        </td>

        {/* Tools */}
        <td className="px-3 py-3">
          <Text small muted>
            {toolCount}
          </Text>
        </td>

        {/* Created */}
        <td className="px-3 py-3">
          <Text small muted>
            {formatDate(createdAt)}
          </Text>
        </td>

        {/* Updated */}
        <td className="px-3 py-3">
          <Text small muted>
            {formatDate(updatedAt)}
          </Text>
        </td>

        {/* Health */}
        <td className="px-3 py-3">
          {causingFailure && (
            <div className="text-destructive flex items-center gap-1.5">
              <CircleAlertIcon className="size-3.5" />
              <Text small className="text-destructive">
                Error
              </Text>
            </div>
          )}
        </td>

        {/* Actions */}
        <td className="px-3 py-3">
          {actions.length > 0 && (
            <div
              className="relative z-20 flex items-center justify-end"
              onClick={(e) => e.stopPropagation()}
            >
              <MoreActions actions={actions} />
            </div>
          )}
        </td>
      </DotRow>
    </TableRowContextMenu>
  );
}
