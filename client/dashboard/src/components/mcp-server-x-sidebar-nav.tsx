import {
  McpSidebarInfoLabel,
  McpSidebarNavShell,
  type McpSidebarNavItem,
} from "@/components/mcp-sidebar-nav-shell";
import {
  McpServerReadinessBar,
  type ReadinessCheck,
} from "@/components/mcp-server-readiness-bar";
import { CopyButton } from "@/components/ui/copy-button";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import {
  getMcpServerArgs,
  remoteMcpRouteParam,
  tunneledMcpRouteParam,
} from "@/lib/sources";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import { MCPServerStatusDropdown } from "@/pages/mcp/x/MCPServerDetails";
import {
  activeTabFromPath,
  mcpServerTabHref,
} from "@/pages/mcp/x/MCPServerDetailsRouting";
import { MCP_AUTHENTICATION_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/authentication/AuthenticationSection";
import { useAllRemoteSessionClients } from "@/pages/mcp/x/tabs/settings/sections/authentication/useAllRemoteSessionClients";
import { MCP_SERVER_URL_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/ServerUrlSection";
import { useRoutes } from "@/routes";
import { useGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { useGetRemoteMcpServer } from "@gram/client/react-query/getRemoteMcpServer.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { usePlugins } from "@gram/client/react-query/plugins";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import {
  ArrowRight,
  ExternalLink,
  LayoutDashboard,
  Settings as SettingsIcon,
  Users,
  Wrench,
} from "lucide-react";
import * as React from "react";
import { useLocation, useParams } from "react-router";

export function McpServerXSidebarNav(): React.JSX.Element | null {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const location = useLocation();
  const { mcpServerSlug } = useParams<{ mcpServerSlug: string }>();
  const isRbacEnabled = telemetry.isFeatureEnabled("gram-rbac") ?? false;

  const idOrSlug = mcpServerSlug ?? "";
  const { data: mcpServer } = useGetMcpServer(
    getMcpServerArgs(idOrSlug),
    undefined,
    { enabled: idOrSlug !== "" },
  );
  const mcpServerId = mcpServer?.id ?? "";
  const { data: endpointsResult, isLoading: isLoadingEndpoints } =
    useMcpEndpoints({ mcpServerId }, undefined, {
      enabled: mcpServerId !== "",
    });
  const endpoints = endpointsResult?.mcpEndpoints ?? [];
  const { mcpUrl, installPageUrl } = useResolvedMcpServerUrl(
    endpoints,
    isLoadingEndpoints,
  );

  const remoteMcpServerId = mcpServer?.remoteMcpServerId ?? "";
  const { data: remoteMcpServer } = useGetRemoteMcpServer(
    { id: remoteMcpServerId },
    undefined,
    { enabled: remoteMcpServerId !== "" },
  );
  const upstreamUrl = remoteMcpServer?.url;

  const userSessionIssuerId = mcpServer?.userSessionIssuerId;
  // A remote identity provider is attached when this server's issuer has at
  // least one remote session client pairing.
  const { items: remoteSessionClients } = useAllRemoteSessionClients(
    { userSessionIssuerId },
    { enabled: !!userSessionIssuerId },
  );
  const hasRemoteIdentityProvider = remoteSessionClients.length > 0;

  // Mirrors PluginStatusBanner's isTrulyPublished: server membership in a
  // plugin alone isn't "included" if the marketplace repo was never
  // published, since a teammate can't actually install it yet.
  const { data: pluginsData } = usePlugins();
  const { data: publishStatus } = usePublishStatus();
  const memberPlugins = (pluginsData?.plugins ?? []).filter((plugin) =>
    plugin.servers?.some((s) => s.mcpServerId === mcpServer?.id),
  );
  const isPluginMember = memberPlugins.length > 0;
  const marketplaceReady = !!(
    publishStatus?.repoOwner && publishStatus.repoName
  );
  const isTrulyIncluded = isPluginMember && marketplaceReady;

  if (!idOrSlug) return null;

  const activeTab = activeTabFromPath(location.pathname, idOrSlug);
  const isRemoteBacked = !!mcpServer?.remoteMcpServerId;
  const isTunneledBacked = !!mcpServer?.tunneledMcpServerId;
  const isSourceBacked = isRemoteBacked || isTunneledBacked;

  let authenticationDescription =
    "Attach a remote identity provider so users can access the upstream service.";
  if (hasRemoteIdentityProvider) {
    authenticationDescription =
      "A remote identity provider is attached to this server.";
  } else if (isTunneledBacked) {
    authenticationDescription =
      "Speakeasy authentication is configured; upstream identity providers are optional.";
  }

  let sourceDescription = "Connect an MCP server as this server's source.";
  let sourceHref = routes.sources.href();
  if (mcpServer?.remoteMcpServerId) {
    sourceDescription = "Backed by a remote MCP server.";
    sourceHref = routes.sources.source.href(
      "remotemcp",
      remoteMcpRouteParam({ id: mcpServer.remoteMcpServerId }),
    );
  } else if (mcpServer?.tunneledMcpServerId) {
    sourceDescription = "Backed by a tunneled MCP server.";
    sourceHref = routes.sources.source.href(
      "tunneledmcp",
      tunneledMcpRouteParam({ id: mcpServer.tunneledMcpServerId }),
    );
  }

  const readinessChecks: ReadinessCheck[] = mcpServer
    ? [
        {
          key: "server-url",
          label: "Server URL",
          description: mcpUrl
            ? "Endpoint is live and ready to connect to."
            : "Add an endpoint so this server has a URL to connect to.",
          ready: !!mcpUrl,
          href: `${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_SERVER_URL_SECTION_ID}`,
        },
        {
          key: "authentication",
          label: "Authentication",
          description: authenticationDescription,
          ready:
            hasRemoteIdentityProvider ||
            (isTunneledBacked && !!userSessionIssuerId),
          href: `${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_AUTHENTICATION_SECTION_ID}`,
        },
        {
          key: "source",
          label: "Source",
          description: sourceDescription,
          ready: isSourceBacked,
          href: sourceHref,
        },
        {
          key: "plugin",
          label: "Included in Plugin",
          description: isTrulyIncluded
            ? `Published to ${memberPlugins.length} plugin${memberPlugins.length > 1 ? "s" : ""}.`
            : isPluginMember
              ? "Marketplace needs publishing before this plugin is installable."
              : "Add this server to a plugin so your team can install it.",
          ready: isTrulyIncluded,
          href: routes.plugins.href(),
        },
      ]
    : [];

  const items: McpSidebarNavItem[] = [
    {
      key: "overview",
      title: "Overview",
      Icon: LayoutDashboard,
      href: mcpServerTabHref(routes, idOrSlug, "overview"),
      active: activeTab === "overview",
    },
    {
      key: "tools",
      title: "Tools",
      Icon: Wrench,
      href: mcpServerTabHref(routes, idOrSlug, "tools"),
      active: activeTab === "tools",
    },
    ...(isRbacEnabled
      ? [
          {
            key: "team-access",
            title: "Team Access",
            Icon: Users,
            href: mcpServerTabHref(routes, idOrSlug, "team-access"),
            active: activeTab === "team-access",
          },
        ]
      : []),
    {
      key: "settings",
      title: "Settings",
      Icon: SettingsIcon,
      href: mcpServerTabHref(routes, idOrSlug, "settings"),
      active: activeTab === "settings",
    },
  ];

  const cardContent = mcpServer && (
    <>
      <div className="flex flex-col gap-0.5">
        <Type className="truncate font-semibold">
          {mcpServer.name || "MCP Server"}
        </Type>
        {isRemoteBacked && (
          <McpSidebarInfoLabel>Remote MCP</McpSidebarInfoLabel>
        )}
        {isTunneledBacked && (
          <McpSidebarInfoLabel>Tunneled MCP</McpSidebarInfoLabel>
        )}
      </div>

      <div className="flex flex-col gap-1.5">
        <McpSidebarInfoLabel>Visibility</McpSidebarInfoLabel>
        <MCPServerStatusDropdown server={mcpServer} />
      </div>

      {mcpUrl && (
        <div className="flex flex-col gap-1">
          <McpSidebarInfoLabel>URL</McpSidebarInfoLabel>
          <div className="flex items-start gap-1">
            <Type
              variant="small"
              muted
              className="line-clamp-2 font-mono text-xs break-all"
            >
              {mcpUrl.replace(/^https?:\/\//, "")}
            </Type>
            <CopyButton
              text={mcpUrl}
              size="inline"
              tooltip="Copy URL"
              className="mt-[-2px] shrink-0"
            />
          </div>
        </div>
      )}

      {upstreamUrl && (
        <div className="flex flex-col gap-1">
          <McpSidebarInfoLabel>Upstream URL</McpSidebarInfoLabel>
          <div className="flex items-start gap-1">
            <Type
              variant="small"
              muted
              className="line-clamp-2 font-mono text-xs break-all"
            >
              {upstreamUrl.replace(/^https?:\/\//, "")}
            </Type>
            <CopyButton
              text={upstreamUrl}
              size="inline"
              tooltip="Copy upstream URL"
              className="mt-[-2px] shrink-0"
            />
          </div>
        </div>
      )}

      <div className="border-border flex items-stretch border-t pt-3">
        {installPageUrl ? (
          <a
            href={installPageUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-muted-foreground hover:text-foreground flex flex-1 items-center justify-center gap-1 text-xs font-semibold transition-colors hover:no-underline"
          >
            Installation page
            <ExternalLink className="h-3 w-3" />
          </a>
        ) : (
          <span className="text-muted-foreground/50 flex flex-1 cursor-not-allowed items-center justify-center gap-1 text-xs font-semibold">
            Installation page
            <ExternalLink className="h-3 w-3" />
          </span>
        )}
        <div className="bg-border w-px self-stretch" />
        <routes.playground.Link
          queryParams={
            isRemoteBacked || isTunneledBacked
              ? { mcpServer: mcpServer.id }
              : undefined
          }
          className="flex flex-1 items-center justify-center hover:no-underline"
        >
          <span className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs font-semibold transition-colors">
            Test in Playground
            <ArrowRight className="h-3 w-3" />
          </span>
        </routes.playground.Link>
      </div>
    </>
  );

  return (
    <McpSidebarNavShell
      backHref={routes.mcp.href()}
      topTitle="Readiness"
      topContent={
        readinessChecks.length > 0 ? (
          <McpServerReadinessBar checks={readinessChecks} />
        ) : undefined
      }
      cardContent={cardContent}
      items={items}
    />
  );
}
