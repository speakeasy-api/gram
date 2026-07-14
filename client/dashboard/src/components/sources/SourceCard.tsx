import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
import { CardContextMenu } from "@/components/card-context-menu";
import { Card } from "@/components/ui/card";
import { MoreActions } from "@/components/ui/more-actions";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useRBAC } from "@/hooks/useRBAC";
import {
  formatRemoteMcpUrlForDisplay,
  formatTunneledMcpDisplay,
  sourceTypeToUrnKind,
} from "@/lib/sources";
import { useRoutes } from "@/routes";
import { Asset } from "@gram/client/models/components/asset.js";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { HoverCardPortal } from "@radix-ui/react-hover-card";
import { Badge } from "@/components/ui/badge";
import { ArrowRight, CircleAlertIcon, FileCode, Network } from "lucide-react";

export type NamedAsset =
  | (Asset & {
      deploymentAssetId: string;
      name: string;
      slug: string;
      type: "openapi" | "function";
    })
  | {
      id: string;
      deploymentAssetId: string;
      name: string;
      slug: string;
      type: "externalmcp";
      organizationMcpCollectionRegistryId?: string;
      registryId?: string;
      iconUrl?: string;
    }
  | {
      id: string;
      deploymentAssetId: string;
      slug: string;
      name?: string | null;
      url: string;
      type: "remotemcp";
    }
  | {
      id: string;
      deploymentAssetId: string;
      slug: string;
      name: string;
      type: "tunneledmcp";
      createdAt?: Date;
      updatedAt?: Date;
    };

const sourceTypeConfig = {
  openapi: {
    label: "OpenAPI",
  },
  function: {
    label: "Function",
  },
  externalmcp: {
    label: "Catalog",
  },
  remotemcp: {
    label: "Remote MCP",
  },
  tunneledmcp: {
    label: "Tunneled MCP",
  },
};

export function SourceCard({
  asset,
  causingFailure,
  deploymentId,
  handleRemove,
  handleViewAsset,
  setChangeDocumentTargetSlug,
}: {
  asset: NamedAsset;
  causingFailure?: boolean | undefined;
  deploymentId?: string;
  handleRemove: (assetId: string) => void;
  handleViewAsset: (assetId: string) => void;
  setChangeDocumentTargetSlug: (slug: string) => void;
}): JSX.Element {
  const routes = useRoutes();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("project:write");
  const config = sourceTypeConfig[asset.type];
  const sourceTypeLabel =
    asset.type === "externalmcp" && asset.organizationMcpCollectionRegistryId
      ? "Collection"
      : config.label;

  const sourceKind = sourceTypeToUrnKind(asset.type);

  // Remote/tunneled MCP deletion lives in Settings because it touches linked server/endpoint state.
  const actions =
    asset.type === "remotemcp" || asset.type === "tunneledmcp"
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

  const remoteMcpUrlDisplay =
    asset.type === "remotemcp"
      ? formatRemoteMcpUrlForDisplay(asset.url)
      : undefined;
  const remoteMcpTrimmedName =
    asset.type === "remotemcp" ? asset.name?.trim() : undefined;
  const displayName =
    asset.type === "remotemcp"
      ? remoteMcpTrimmedName || remoteMcpUrlDisplay || ""
      : asset.type === "tunneledmcp"
        ? formatTunneledMcpDisplay(asset)
        : asset.name;
  const displaySubtitle =
    asset.type === "remotemcp" && remoteMcpTrimmedName
      ? remoteMcpUrlDisplay
      : undefined;

  const iconContent = (() => {
    if (asset.type === "externalmcp" && asset.iconUrl) {
      return (
        <img
          src={asset.iconUrl}
          alt={asset.name}
          className="h-12 w-12 object-contain"
        />
      );
    }
    if (
      asset.type === "externalmcp" ||
      asset.type === "remotemcp" ||
      asset.type === "tunneledmcp"
    ) {
      return <Network className="text-muted-foreground h-8 w-8" />;
    }
    return <FileCode className="text-muted-foreground h-8 w-8" />;
  })();

  return (
    <CardContextMenu actions={actions}>
      <routes.sources.source.Link
        key={asset.id}
        params={[sourceKind, asset.slug]}
        className="block h-full hover:no-underline"
      >
        <Card icon={iconContent}>
          {/* Header row with name and actions */}
          <div className="mb-2 flex items-start justify-between gap-2">
            <div className="min-w-0 flex-1">
              <Type
                variant="subheading"
                as="div"
                className="text-md group-hover:text-primary truncate transition-colors"
                title={displayName}
              >
                {displayName}
              </Type>
              {displaySubtitle && (
                <Type
                  as="div"
                  muted
                  small
                  className="truncate"
                  title={displaySubtitle}
                >
                  {displaySubtitle}
                </Type>
              )}
            </div>
            <div className="flex shrink-0 items-center gap-1">
              {causingFailure && <AssetIsCausingFailureNotice />}
              {actions.length > 0 && (
                <div onClick={(e) => e.stopPropagation()}>
                  <MoreActions actions={actions} />
                </div>
              )}
            </div>
          </div>

          {/* Footer row with type badge and open link */}
          <div className="mt-auto flex items-center justify-between gap-2 pt-2">
            <Badge variant="neutral">{sourceTypeLabel}</Badge>
            <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
              <span>Open</span>
              <ArrowRight className="h-3.5 w-3.5" />
            </div>
          </div>
        </Card>
      </routes.sources.source.Link>
    </CardContextMenu>
  );
}

export function SourceCardSkeleton(): JSX.Element {
  return (
    <div className="bg-card text-card-foreground flex flex-row overflow-hidden border">
      {/* Dot pattern sidebar placeholder */}
      <Skeleton className="w-40 shrink-0 rounded-none border-r" />

      {/* Content area */}
      <div className="flex flex-1 flex-col p-4">
        {/* Name placeholder */}
        <Skeleton className="mb-2 h-5 w-2/3" />

        {/* Footer row */}
        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <Skeleton className="h-5 w-16" />
          <Skeleton className="h-4 w-24" />
        </div>
      </div>
    </div>
  );
}

const AssetIsCausingFailureNotice = () => {
  const latestDeployment = useLatestDeployment();
  const routes = useRoutes();

  return (
    <HoverCard>
      <HoverCardTrigger
        className="cursor-pointer"
        aria-label="View deployment failure details"
      >
        <CircleAlertIcon className="text-destructive size-3" />
      </HoverCardTrigger>
      <HoverCardPortal>
        <HoverCardContent side="bottom" className="text-sm" asChild>
          <div>
            <div>
              This API source caused the latest deployment to fail. Remove or
              update it to prevent future failures.
            </div>
            <div className="mt-3 flex justify-end">
              <routes.deployments.deployment.Link
                className="text-link"
                params={[latestDeployment.data?.deployment?.id ?? ""]}
              >
                View Logs
              </routes.deployments.deployment.Link>
            </div>
          </div>
        </HoverCardContent>
      </HoverCardPortal>
    </HoverCard>
  );
};
