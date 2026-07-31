import { CopyButton } from "@/components/ui/CopyButton";
import { DotRow } from "@/components/ui/DotRow";
import { Text } from "@/components/ui/Text";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { AlertTriangleIcon, Link2, Network, Package } from "lucide-react";
import { useMemo } from "react";
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

// MCPServerTableRow renders one mcp_servers row in the /mcp listing table
// view. Mirrors MCPServerCard.
export function MCPServerTableRow({
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

  const { mcpUrl } = useResolvedMcpServerUrl(endpoints, isLoadingEndpoints);
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
  const handleClick = () => {
    // A catalog server that requires OAuth but has none configured lands on
    // the Authentication tab so setup is one click away.
    if (oauthSetupNeeded) {
      routes.mcp.x.authentication.goTo(routeParam);
    } else {
      routes.mcp.x.overview.goTo(routeParam);
    }
  };

  const mcpEnabled = server.visibility !== "disabled";
  const mcpIsPublic = server.visibility === "public";

  return (
    <DotRow
      onClick={handleClick}
      icon={
        externalMcpLogoUrl ? (
          <img
            src={externalMcpLogoUrl}
            alt={displayName}
            className="h-6 w-6 object-contain"
          />
        ) : (
          <Network className="text-muted-foreground h-5 w-5" />
        )
      }
    >
      {/* Name */}
      <td className="px-3 py-3">
        <div className="flex items-center gap-2">
          <Text
            variant="subheading"
            as="div"
            className="group-hover:text-primary min-w-0 flex-1 truncate text-sm transition-colors"
            title={displayName}
          >
            {displayName}
          </Text>
          {oauthSetupNeeded && (
            <Badge variant="warning">
              <Badge.LeftIcon>
                <AlertTriangleIcon />
              </Badge.LeftIcon>
              <Badge.Text>OAuth Required</Badge.Text>
            </Badge>
          )}
          {activityStatus && (
            <MCPActivityIndicator
              status={activityStatus}
              recentWindowDays={recentWindowDays}
              size="sm"
              className="shrink-0"
            />
          )}
        </div>
      </td>

      {/* Status */}
      <td className="px-3 py-3">
        <MCPStatusIndicator
          mcpEnabled={mcpEnabled}
          mcpIsPublic={mcpIsPublic}
          size="sm"
        />
      </td>

      {/* URL */}
      <td className="max-w-xs px-3 py-3">
        {mcpUrl ? (
          <div className="flex items-center gap-1.5">
            <Text small muted className="truncate">
              {mcpUrl}
            </Text>
            <CopyButton
              text={mcpUrl}
              size="sm"
              icon={Link2}
              tooltip="Copy MCP URL"
            />
            {installSourceTooltip && (
              <Button
                type="button"
                variant="tertiary"
                size="sm"
                tooltip={installSourceTooltip}
                aria-label={installSourceTooltip}
                onClick={(e) => e.stopPropagation()}
              >
                <Package className="text-muted-foreground group-hover:text-foreground h-4 w-4" />
              </Button>
            )}
          </div>
        ) : (
          <Text small muted>
            —
          </Text>
        )}
      </td>

      {/* Tools */}
      <td className="px-3 py-3">
        {isToolsetBacked ? (
          <ToolCollectionBadge
            toolNames={toolNames}
            emptyLabel={isExternalMcpProxy ? null : undefined}
          />
        ) : (
          <Badge variant="neutral">MCP Server</Badge>
        )}
      </td>
    </DotRow>
  );
}

export function MCPServerTableRowSkeleton(): JSX.Element {
  return (
    <DotRow>
      <td className="px-3 py-3">
        <div className="bg-muted h-4 w-2/3 animate-pulse rounded" />
      </td>
      <td className="px-3 py-3">
        <div className="flex items-center gap-2">
          <div className="bg-muted h-2 w-2 animate-pulse rounded-full" />
          <div className="bg-muted h-3.5 w-12 animate-pulse rounded" />
        </div>
      </td>
      <td className="px-3 py-3">
        <div className="bg-muted h-3.5 w-40 animate-pulse rounded" />
      </td>
      <td className="px-3 py-3">
        <div className="bg-muted h-5 w-10 animate-pulse rounded-full" />
      </td>
    </DotRow>
  );
}
