import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
import { MoreActions } from "@/components/ui/more-actions";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { UpdatedAt } from "@/components/updated-at";
import { useRoutes } from "@/routes";
import { Asset } from "@gram/client/models/components";
import { useLatestDeployment } from "@gram/client/react-query/index.js";
import { HoverCardPortal } from "@radix-ui/react-hover-card";
import { Badge } from "@speakeasy-api/moonshine";
import { CircleAlertIcon } from "lucide-react";
import {
  ExternalMCPIllustration,
  FunctionIllustration,
  OpenAPIIllustration,
} from "./SourceCardIllustrations";

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
  handleRemove,
  handleViewAsset,
  setChangeDocumentTargetSlug,
}: {
  asset: NamedAsset;
  causingFailure?: boolean | undefined;
  handleRemove: (assetId: string) => void;
  handleViewAsset: (assetId: string) => void;
  setChangeDocumentTargetSlug: (slug: string) => void;
}) {
  const routes = useRoutes();
  const config = sourceTypeConfig[asset.type];

  const sourceKind =
    asset.type === "openapi"
      ? "http"
      : asset.type === "function"
        ? "function"
        : "externalmcp";

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
    {
      label: "Delete",
      onClick: () => handleRemove(asset.id),
      icon: "trash" as const,
      destructive: true,
    },
  ];

  let displayName = asset.name;
  let subtitle: React.ReactNode = null;

  if (asset.type === "externalmcp") {
    subtitle = asset.slug;
  } else if ("updatedAt" in asset && asset.updatedAt) {
    subtitle = (
      <UpdatedAt
        date={new Date(asset.updatedAt)}
        italic={false}
        className="text-xs"
        showRecentness
      />
    );
  }

  // Render the appropriate illustration based on source type
  const renderIllustration = () => {
    switch (asset.type) {
      case "openapi":
        return <OpenAPIIllustration />;
      case "function":
        return <FunctionIllustration />;
      case "externalmcp":
        return (
          <ExternalMCPIllustration logoUrl={asset.iconUrl} name={asset.name} />
        );
    }
  };

  return (
    <routes.sources.source.Link
      key={asset.id}
      params={[sourceKind, asset.slug]}
      className="group bg-card text-card-foreground flex flex-col rounded-xl border overflow-hidden hover:border-foreground/20 hover:shadow-md transition-all hover:no-underline"
    >
      {/* Illustration header */}
      <div className="h-36 w-full overflow-hidden border-b">
        {renderIllustration()}
      </div>

      {/* Content area */}
      <div className="p-4 flex flex-col flex-1">
        {/* Header row with name and actions */}
        <div className="flex items-start justify-between gap-2 mb-2">
          <Type
            variant="subheading"
            as="div"
            className="truncate flex-1 text-md group-hover:text-primary transition-colors"
            title={displayName}
          >
            {displayName}
          </Type>
          <div className="flex items-center gap-1 shrink-0">
            {causingFailure && <AssetIsCausingFailureNotice />}
            <div onClick={(e) => e.stopPropagation()}>
              <MoreActions actions={actions} />
            </div>
          </div>
        </div>

        {/* Footer row with type badge and metadata */}
        <div className="flex items-center justify-between gap-2 mt-auto pt-2">
          <Badge variant="neutral">{config.label}</Badge>
          <Type small muted as="span">
            {subtitle}
          </Type>
        </div>
      </div>
    </routes.sources.source.Link>
  );
}

export function SourceCardSkeleton() {
  return (
    <div className="bg-card text-card-foreground flex flex-col rounded-xl border overflow-hidden">
      {/* Illustration header placeholder */}
      <div className="h-36 w-full bg-muted/50 animate-pulse border-b" />

      {/* Content area */}
      <div className="p-4 flex flex-col">
        {/* Name placeholder */}
        <div className="h-5 w-2/3 bg-muted rounded animate-pulse mb-2" />

        {/* Footer row */}
        <div className="flex items-center justify-between gap-2 mt-auto pt-2">
          <div className="h-5 w-16 bg-muted rounded-full animate-pulse" />
          <div className="h-4 w-24 bg-muted rounded animate-pulse" />
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
        <CircleAlertIcon className="size-3 text-destructive" />
      </HoverCardTrigger>
      <HoverCardPortal>
        <HoverCardContent side="bottom" className="text-sm" asChild>
          <div>
            <div>
              This API source caused the latest deployment to fail. Remove or
              update it to prevent future failures.
            </div>
            <div className="flex justify-end mt-3">
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
