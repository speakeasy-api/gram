import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
import { DotCard } from "@/components/ui/dot-card";
import { MoreActions } from "@/components/ui/more-actions";
import { Type } from "@/components/ui/type";
import { sourceTypeToUrnKind } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { Asset } from "@gram/client/models/components";
import { useLatestDeployment } from "@gram/client/react-query/index.js";
import { HoverCardPortal } from "@radix-ui/react-hover-card";
import { Badge } from "@speakeasy-api/moonshine";
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
      registryId: string;
      iconUrl?: string;
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
}) {
  const routes = useRoutes();
  const config = sourceTypeConfig[asset.type];

  const sourceKind = sourceTypeToUrnKind(asset.type);

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
    },
  ];

  const displayName = asset.name;

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
    if (asset.type === "externalmcp") {
      return <Network className="text-muted-foreground h-8 w-8" />;
    }
    return <FileCode className="text-muted-foreground h-8 w-8" />;
  })();

  return (
    <routes.sources.source.Link
      key={asset.id}
      params={[sourceKind, asset.slug]}
      className="hover:no-underline"
    >
      <DotCard icon={iconContent}>
        {/* Header row with name and actions */}
        <div className="mb-2 flex items-start justify-between gap-2">
          <Type
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary flex-1 truncate transition-colors"
            title={displayName}
          >
            {displayName}
          </Type>
          <div className="flex shrink-0 items-center gap-1">
            {causingFailure && <AssetIsCausingFailureNotice />}
            <div onClick={(e) => e.stopPropagation()}>
              <MoreActions actions={actions} />
            </div>
          </div>
        </div>

        {/* Footer row with type badge and open link */}
        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <Badge variant="neutral">{config.label}</Badge>
          <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
            <span>Open</span>
            <ArrowRight className="h-3.5 w-3.5" />
          </div>
        </div>
      </DotCard>
    </routes.sources.source.Link>
  );
}

export function SourceCardSkeleton() {
  return (
    <div className="bg-card text-card-foreground flex flex-row overflow-hidden rounded-xl border">
      {/* Dot pattern sidebar placeholder */}
      <div className="bg-muted/50 w-40 shrink-0 animate-pulse border-r" />

      {/* Content area */}
      <div className="flex flex-1 flex-col p-4">
        {/* Name placeholder */}
        <div className="bg-muted mb-2 h-5 w-2/3 animate-pulse rounded" />

        {/* Footer row */}
        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <div className="bg-muted h-5 w-16 animate-pulse rounded-full" />
          <div className="bg-muted h-4 w-24 animate-pulse rounded" />
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
