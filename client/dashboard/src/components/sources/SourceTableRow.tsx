import { DotRow } from "@/components/ui/dot-row";
import { MoreActions } from "@/components/ui/more-actions";
import { Type } from "@/components/ui/type";
import { useRBAC } from "@/hooks/useRBAC";
import { sourceTypeToUrnKind } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { Badge } from "@speakeasy-api/moonshine";
import { CircleAlertIcon, FileCode, Network } from "lucide-react";
import type { NamedAsset } from "./SourceCard";

const sourceTypeConfig = {
  openapi: { label: "OpenAPI" },
  function: { label: "Function" },
  externalmcp: { label: "Catalog" },
};

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
}) {
  const routes = useRoutes();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("build:write");
  const config = sourceTypeConfig[asset.type];
  const sourceKind = sourceTypeToUrnKind(asset.type);

  const createdAt = "createdAt" in asset ? asset.createdAt : undefined;
  const updatedAt = "updatedAt" in asset ? asset.updatedAt : undefined;

  const actions = [
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
            onClick: () => routes.deployments.deployment.goTo(deploymentId),
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
    if (asset.type === "externalmcp") {
      return <Network className="text-muted-foreground h-5 w-5" />;
    }
    return <FileCode className="text-muted-foreground h-5 w-5" />;
  })();

  return (
    <DotRow
      icon={iconContent}
      onClick={() => routes.sources.source.goTo(sourceKind, asset.slug)}
    >
      {/* Name */}
      <td className="px-3 py-3">
        <Type
          variant="subheading"
          as="div"
          className="group-hover:text-primary truncate text-sm transition-colors"
          title={asset.name}
        >
          {asset.name}
        </Type>
      </td>

      {/* Type */}
      <td className="px-3 py-3">
        <Badge variant="neutral">{config.label}</Badge>
      </td>

      {/* Tools */}
      <td className="px-3 py-3">
        <Type small muted>
          {toolCount}
        </Type>
      </td>

      {/* Created */}
      <td className="px-3 py-3">
        <Type small muted>
          {formatDate(createdAt)}
        </Type>
      </td>

      {/* Updated */}
      <td className="px-3 py-3">
        <Type small muted>
          {formatDate(updatedAt)}
        </Type>
      </td>

      {/* Health */}
      <td className="px-3 py-3">
        {causingFailure && (
          <div className="text-destructive flex items-center gap-1.5">
            <CircleAlertIcon className="size-3.5" />
            <Type small className="text-destructive">
              Error
            </Type>
          </div>
        )}
      </td>

      {/* Actions */}
      <td className="px-3 py-3">
        <div
          className="flex items-center justify-end"
          onClick={(e) => e.stopPropagation()}
        >
          <MoreActions actions={actions} />
        </div>
      </td>
    </DotRow>
  );
}
