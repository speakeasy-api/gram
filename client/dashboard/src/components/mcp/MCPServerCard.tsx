import { CopyButton } from "@/components/ui/CopyButton";
import { DotCard } from "@/components/ui/DotCard";
import { Text } from "@/components/ui/Text";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  AlertTriangleIcon,
  ArrowRight,
  Link2,
  Network,
  Package,
} from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router";
import {
  useCatalogIconMap,
  useExternalMcpOAuthConfigStatus,
} from "../sources/sources-hooks";
import { ToolCollectionBadge } from "../tool-collection-badge";
import {
  externalMcpSlug,
  hasExternalMcpProxy,
  mcpServerDisplayName,
  visibleToolNames,
} from "./mcp-server-listing";
import { MCPActivityIndicator } from "./MCPActivityIndicator";
import { MCPStatusIndicator } from "./MCPStatusIndicator";
import type { McpActivityStatus } from "./mcp-activity";

// MCPServerCard renders one mcp_servers row inside the /mcp listing grid.
// Toolset-backed rows hydrate hosted-card details (tool badge, catalog
// branding, OAuth readiness) from the server's toolset summary; remote and
// tunneled rows show their endpoint count instead.
export function MCPServerCard({
  server,
  endpoints,
  isLoadingEndpoints,
  activityStatus,
  recentWindowDays,
}: {
  server: McpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
  activityStatus?: McpActivityStatus | null;
  recentWindowDays?: number;
}): JSX.Element {
  const routes = useRoutes();
  const summary = server.toolsetSummary;
  const isToolsetBacked = server.backendKind === "toolset" && !!summary;

  const { installPageUrl } = useResolvedMcpServerUrl(
    endpoints,
    isLoadingEndpoints,
  );
  const catalogIconMap = useCatalogIconMap();
  const { data: deploymentResult } = useLatestDeployment();
  const oauthStatus = useExternalMcpOAuthConfigStatus(
    isToolsetBacked ? summary.slug : undefined,
  );
  const oauthSetupNeeded = oauthStatus === "required-unconfigured";

  const displayName = mcpServerDisplayName(server);

  const externalMcpLogoUrl = useMemo(() => {
    const slug = externalMcpSlug(summary);
    if (!slug) return undefined;

    const matchingMcp = deploymentResult?.deployment?.externalMcps?.find(
      (mcp) => mcp.slug === slug,
    );
    return matchingMcp?.registryServerSpecifier
      ? catalogIconMap.get(matchingMcp.registryServerSpecifier)
      : undefined;
  }, [summary, catalogIconMap, deploymentResult]);

  const installSourceTooltip = summary?.originRegistrySpecifier
    ? `Installed from ${summary.originRegistrySpecifier}`
    : undefined;

  const toolNames = visibleToolNames(summary);
  const isExternalMcpProxy = hasExternalMcpProxy(summary);

  const routeParam = mcpServerRouteParam(server);
  // A catalog server that requires OAuth but has none configured lands on the
  // Authentication tab so setup is one click away.
  const href = oauthSetupNeeded
    ? routes.mcp.x.authentication.href(routeParam)
    : routes.mcp.x.overview.href(routeParam);

  const mcpEnabled = server.visibility !== "disabled";
  const mcpIsPublic = server.visibility === "public";

  return (
    <Link
      to={href}
      className="focus-visible:ring-ring block rounded-xl no-underline focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
    >
      <DotCard
        overlay={
          oauthSetupNeeded && (
            <div className="absolute bottom-3.5 left-1/2 z-10 -translate-x-1/2">
              <Badge variant="warning">
                <Badge.LeftIcon>
                  <AlertTriangleIcon />
                </Badge.LeftIcon>
                <Badge.Text>OAuth Required</Badge.Text>
              </Badge>
            </div>
          )
        }
        icon={
          externalMcpLogoUrl ? (
            <img
              src={externalMcpLogoUrl}
              alt={displayName}
              className="h-12 w-12 object-contain"
            />
          ) : (
            <Network className="text-muted-foreground h-8 w-8" />
          )
        }
      >
        {/* Header row with name */}
        <div className="mb-2 flex items-start justify-between gap-2">
          <Text
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary flex-1 truncate transition-colors"
            title={displayName}
          >
            {displayName}
          </Text>
          <div className="flex items-center gap-1">
            {installPageUrl && (
              <CopyButton
                text={installPageUrl}
                size="sm"
                icon={Link2}
                tooltip="Copy install page URL"
              />
            )}
            {installSourceTooltip && (
              <Button
                type="button"
                variant="tertiary"
                size="sm"
                tooltip={installSourceTooltip}
                aria-label={installSourceTooltip}
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                }}
              >
                <Package className="text-muted-foreground group-hover:text-foreground h-4 w-4" />
              </Button>
            )}
            {isToolsetBacked ? (
              <ToolCollectionBadge
                toolNames={toolNames}
                emptyLabel={isExternalMcpProxy ? null : undefined}
              />
            ) : (
              <Badge variant="neutral" className="bg-card">
                <Badge.Text>
                  {endpoints.length}{" "}
                  {endpoints.length === 1 ? "endpoint" : "endpoints"}
                </Badge.Text>
              </Badge>
            )}
          </div>
        </div>

        {/* Footer row with status indicator and open link */}
        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <div className="flex items-center gap-2">
            <MCPStatusIndicator
              mcpEnabled={mcpEnabled}
              mcpIsPublic={mcpIsPublic}
            />
            {activityStatus && (
              <MCPActivityIndicator
                status={activityStatus}
                recentWindowDays={recentWindowDays}
              />
            )}
          </div>
          {oauthSetupNeeded ? (
            <div className="text-warning flex items-center gap-1 text-sm">
              <span>Set up</span>
              <ArrowRight className="h-3.5 w-3.5" />
            </div>
          ) : (
            <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
              <span>Open</span>
              <ArrowRight className="h-3.5 w-3.5" />
            </div>
          )}
        </div>
      </DotCard>
    </Link>
  );
}

export function MCPServerCardSkeleton(): JSX.Element {
  return (
    <DotCard>
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="bg-muted h-5 w-2/3 animate-pulse rounded" />
        <div className="bg-muted h-5 w-10 animate-pulse rounded-full" />
      </div>
      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <div className="flex items-center gap-2">
          <div className="bg-muted h-2.5 w-2.5 animate-pulse rounded-full" />
          <div className="bg-muted h-3.5 w-12 animate-pulse rounded" />
        </div>
        <div className="bg-muted h-3.5 w-10 animate-pulse rounded" />
      </div>
    </DotCard>
  );
}
