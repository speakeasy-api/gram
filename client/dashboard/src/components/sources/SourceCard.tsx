import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/HoverCard";
import { CardContextMenu } from "@/components/card-context-menu";
import { Card } from "@/components/ui/Card";
import { MoreActions } from "@/components/ui/MoreActions";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import {
  formatRemoteMcpUrlForDisplay,
  formatTunneledMcpDisplay,
  sourceTypeToUrnKind,
} from "@/lib/sources";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Asset } from "@gram/client/models/components/asset.js";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { HoverCardPortal } from "@radix-ui/react-hover-card";
import { Badge } from "@/components/ui/Badge";
import { ArrowRight, CircleAlertIcon, FileCode, Network } from "lucide-react";
import { AssetImage } from "@/components/asset-image";

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
      transportType?: string;
      mcpServerId?: string;
    }
  | {
      id: string;
      deploymentAssetId: string;
      slug: string;
      name: string;
      type: "tunneledmcp";
      createdAt?: Date;
      updatedAt?: Date;
      mcpServerId?: string;
    }
  | {
      id: string;
      deploymentAssetId: string;
      slug: string;
      name?: string | null;
      url: string;
      type: "unproxiedmcp";
      mcpServerId?: string;
    };

// sourceCardNameAndSubtitle centralizes the "what to render" logic for
// source types whose display name falls back to a URL when unnamed
// (remotemcp, unproxiedmcp), keeping the branching out of the component
// body as a flat switch instead of a nested ternary.
function sourceCardNameAndSubtitle(asset: NamedAsset): {
  displayName: string | undefined;
  displaySubtitle: string | undefined;
} {
  switch (asset.type) {
    case "remotemcp": {
      const urlDisplay = formatRemoteMcpUrlForDisplay(asset.url);
      const trimmedName = asset.name?.trim();
      return {
        displayName: trimmedName || urlDisplay || "",
        displaySubtitle: trimmedName ? urlDisplay : undefined,
      };
    }
    case "unproxiedmcp": {
      const urlDisplay = formatRemoteMcpUrlForDisplay(asset.url);
      const trimmedName = asset.name?.trim();
      return {
        displayName: trimmedName || urlDisplay || "",
        displaySubtitle: trimmedName ? urlDisplay : undefined,
      };
    }
    case "tunneledmcp":
      return {
        displayName: formatTunneledMcpDisplay(asset),
        displaySubtitle: undefined,
      };
    case "openapi":
    case "function":
    case "externalmcp":
      return { displayName: asset.name, displaySubtitle: undefined };
  }
}

// SourceMcpIcon looks up the real server icon for remote/tunneled/unproxied
// sources (mcp_metadata.logo_id, keyed by the wrapping mcp_server, not the
// source row itself) and falls back to the generic Network icon when there's
// no wrapping mcp_server yet or no icon has been set on it.
export function SourceMcpIcon({
  mcpServerId,
  className,
}: {
  mcpServerId: string | undefined;
  className: string;
}): JSX.Element {
  const { data } = useGetMcpMetadata({ mcpServerId }, undefined, {
    enabled: !!mcpServerId,
    // 404 is the normal "no metadata set" answer; don't re-request it on every
    // remount or each navigation replays a console error per server.
    retry: false,
    retryOnMount: false,
    staleTime: 5 * 60 * 1000,
    throwOnError: false,
  });
  const logoAssetId = data?.metadata?.logoAssetId;

  if (logoAssetId) {
    return <AssetImage assetId={logoAssetId} className={className} />;
  }
  return <Network className={cn("text-muted-foreground", className)} />;
}

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
  unproxiedmcp: {
    label: "Unproxied MCP",
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
  const sourceTypeLabel = config.label;

  const sourceKind = sourceTypeToUrnKind(asset.type);

  // Remote/tunneled/unproxied MCP deletion lives in Settings because it
  // touches linked server/endpoint state.
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

  const { displayName, displaySubtitle } = sourceCardNameAndSubtitle(asset);

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
      asset.type === "remotemcp" ||
      asset.type === "tunneledmcp" ||
      asset.type === "unproxiedmcp"
    ) {
      return (
        <SourceMcpIcon
          mcpServerId={asset.mcpServerId}
          className="h-8 w-8 object-contain"
        />
      );
    }
    if (asset.type === "externalmcp") {
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
        <Card.Entity icon={iconContent}>
          {/* Header row with name and actions */}
          <div className="mb-2 flex items-start justify-between gap-2">
            <div className="min-w-0 flex-1">
              <Text
                variant="subheading"
                as="div"
                className="text-md group-hover:text-primary truncate transition-colors"
                title={displayName}
              >
                {displayName}
              </Text>
              {displaySubtitle && (
                <Text
                  as="div"
                  muted
                  small
                  className="truncate"
                  title={displaySubtitle}
                >
                  {displaySubtitle}
                </Text>
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
        </Card.Entity>
      </routes.sources.source.Link>
    </CardContextMenu>
  );
}

export function SourceCardSkeleton(): JSX.Element {
  return (
    <div className="bg-card text-card-foreground flex flex-row overflow-hidden border">
      {/* Dot pattern sidebar placeholder */}
      <div className="bg-muted/50 w-40 shrink-0 animate-pulse border-r" />

      {/* Content area */}
      <div className="flex flex-1 flex-col p-4">
        {/* Name placeholder */}
        <div className="bg-muted mb-2 h-5 w-2/3 animate-pulse" />

        {/* Footer row */}
        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <div className="bg-muted h-5 w-16 animate-pulse rounded-full" />
          <div className="bg-muted h-4 w-24 animate-pulse" />
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
