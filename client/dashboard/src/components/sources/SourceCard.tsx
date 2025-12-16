import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
import { ExternalMcpIcon } from "@/components/ui/mcp-icon";
import { MoreActions } from "@/components/ui/more-actions";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { UpdatedAt } from "@/components/updated-at";
import { useRoutes } from "@/routes";
import { Asset } from "@gram/client/models/components";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import {
  useGetSourceEnvironment,
  useLatestDeployment,
} from "@gram/client/react-query/index.js";
import { HoverCardPortal } from "@radix-ui/react-hover-card";
import { Badge } from "@speakeasy-api/moonshine";
import { CircleAlertIcon, FileCode, Globe, SquareFunction } from "lucide-react";

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
    };

export function SourceCard({
  asset,
  causingFailure,
  handleRemove,
  handleAttachEnvironment,
  handleViewAsset,
  setChangeDocumentTargetSlug,
}: {
  asset: NamedAsset;
  causingFailure?: boolean | undefined;
  handleRemove: (assetId: string) => void;
  handleAttachEnvironment: (assetId: string) => void;
  handleViewAsset: (assetId: string) => void;
  setChangeDocumentTargetSlug: (slug: string) => void;
}) {
  const routes = useRoutes();
  const IconComponent =
    asset.type === "openapi"
      ? FileCode
      : asset.type === "function"
        ? SquareFunction
        : ExternalMcpIcon;

  const sourceKind =
    asset.type === "openapi"
      ? "http"
      : asset.type === "function"
        ? "function"
        : "externalmcp";

  // Check if environment is attached (not applicable for external MCPs)
  const sourceEnvironment = useGetSourceEnvironment(
    {
      sourceKind: sourceKind as "http" | "function",
      sourceSlug: asset.slug,
    },
    undefined,
    {
      enabled: asset.type !== "externalmcp",
      retry: (_, err) => {
        if (err instanceof GramError && err.statusCode === 404) {
          return false;
        }
        return true;
      },
      throwOnError: false,
    },
  );

  const hasEnvironment =
    asset.type !== "externalmcp" && !!sourceEnvironment.data?.id;

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
    ...(asset.type !== "externalmcp"
      ? [
          {
            label: "Attach Environment",
            onClick: () => handleAttachEnvironment(asset.id),
            icon: "globe" as const,
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
  let footer =
    "updatedAt" in asset && asset.updatedAt ? (
      <UpdatedAt
        date={new Date(asset.updatedAt)}
        italic={false}
        className="text-xs"
        showRecentness
      />
    ) : null;

  if (asset.type === "externalmcp") {
    displayName = asset.slug;
    footer = (
      <Type muted className="text-xs">
        {asset.name}
      </Type>
    );
  }

  const cardContent = (
    <>
      <div className="flex items-center justify-between mb-2">
        <IconComponent className="size-5 shrink-0" strokeWidth={2} />
        <div onClick={(e) => e.stopPropagation()}>
          <MoreActions actions={actions} />
        </div>
      </div>

      <div className="leading-none mb-1.5 flex items-center justify-between flex-wrap">
        <Type className="overflow-hidden text-ellipsis whitespace-nowrap max-w-[200px] [direction:rtl]">
          <span className="[direction:ltr] [unicode-bidi:embed]">
            {displayName}
          </span>
        </Type>
        {hasEnvironment && (
          <SimpleTooltip
            tooltip={`Attached environment: ${sourceEnvironment.data?.name || "Unknown"}`}
          >
            <Badge className="flex items-center gap-1 text-xs">
              <Globe className="h-3 w-3" />
              Env
            </Badge>
          </SimpleTooltip>
        )}
      </div>

      <div className="flex gap-1.5 items-center text-muted-foreground text-xs">
        {causingFailure && <AssetIsCausingFailureNotice />}
        {footer}
      </div>
    </>
  );

  const cardClassName =
    "bg-secondary max-w-sm text-card-foreground flex flex-col rounded-md border px-3 py-3 hover:brightness-95 transition-colors hover:no-underline";

  // External MCPs don't have a details page yet, so render as a div
  if (asset.type === "externalmcp") {
    return (
      <div key={asset.id} className={cardClassName}>
        {cardContent}
      </div>
    );
  }

  return (
    <routes.sources.source.Link
      key={asset.id}
      params={[sourceKind, asset.slug]}
      className={cardClassName}
    >
      {cardContent}
    </routes.sources.source.Link>
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
