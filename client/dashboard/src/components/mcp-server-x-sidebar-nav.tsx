import {
  DetailSidebarInfoLabel,
  DetailSidebarNav,
  type DetailSidebarNavItem,
} from "@/components/detail/detail-sidebar-nav";
import {
  McpServerReadinessBar,
  type ReadinessCheck,
} from "@/components/mcp-server-readiness-bar";
import { SetupGuideCard } from "@/components/setup-guide/SetupGuideCard";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import {
  getMcpServerArgs,
  remoteMcpRouteParam,
  tunneledMcpRouteParam,
  unproxiedMcpRouteParam,
} from "@/lib/sources";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import { useRBAC } from "@/hooks/useRBAC";
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
import { useGetUnproxiedMcpServer } from "@gram/client/react-query/getUnproxiedMcpServer.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { usePlugins } from "@gram/client/react-query/plugins";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import {
  ArrowRight,
  ExternalLink,
  LayoutDashboard,
  Plug,
  Settings as SettingsIcon,
  Users,
  Wrench,
} from "lucide-react";
import * as React from "react";
import { useLocation, useParams } from "react-router";

export function McpServerXSidebarNav(): React.JSX.Element | null {
  const routes = useRoutes();
  const location = useLocation();
  const { mcpServerSlug } = useParams<{ mcpServerSlug: string }>();
  const { hasScope } = useRBAC();

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
  const unproxiedMcpServerId = mcpServer?.unproxiedMcpServerId ?? "";
  const { data: unproxiedMcpServer } = useGetUnproxiedMcpServer(
    { id: unproxiedMcpServerId },
    undefined,
    { enabled: unproxiedMcpServerId !== "" },
  );
  const upstreamUrl = remoteMcpServer?.url ?? unproxiedMcpServer?.url;

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
  const isUnproxied = !!mcpServer?.unproxiedMcpServerId;
  const isSourceBacked = isRemoteBacked || isTunneledBacked || isUnproxied;
  const canViewTeamAccess =
    !!mcpServer && hasScope("org:read") && hasScope("mcp:read", mcpServer.id);

  let authenticationDescription =
    "Attach a remote identity provider so users can access the upstream service.";
  if (isUnproxied) {
    authenticationDescription =
      "Not applicable — the customer connects directly using the vendor's own credentials.";
  } else if (hasRemoteIdentityProvider) {
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
  } else if (mcpServer?.unproxiedMcpServerId) {
    sourceDescription = "Backed by an unproxied MCP server.";
    sourceHref = routes.sources.source.href(
      "unproxiedmcp",
      unproxiedMcpRouteParam({ id: mcpServer.unproxiedMcpServerId }),
    );
  }

  const readinessChecks: ReadinessCheck[] = mcpServer
    ? [
        {
          key: "server-url",
          label: "Server URL",
          description: isUnproxied
            ? "Not applicable — unproxied servers have no Speakeasy-hosted endpoint."
            : mcpUrl
              ? "Endpoint is live and ready to connect to."
              : "Add an endpoint so this server has a URL to connect to.",
          ready: isUnproxied || !!mcpUrl,
          href: isUnproxied
            ? undefined
            : `${mcpServerTabHref(routes, idOrSlug, "settings")}#${MCP_SERVER_URL_SECTION_ID}`,
        },
        {
          key: "authentication",
          label: "Authentication",
          description: authenticationDescription,
          ready:
            isUnproxied ||
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

  const items: DetailSidebarNavItem[] = [
    {
      key: "overview",
      title: "Overview",
      Icon: LayoutDashboard,
      href: mcpServerTabHref(routes, idOrSlug, "overview"),
      active: activeTab === "overview",
    },
    // Hidden for unproxied servers for now: there's no reliable way to list
    // their tools yet (the vendor's own auth blocks an anonymous probe, and
    // there's no fallback source wired up), so the tab had nothing useful to
    // show.
    ...(isUnproxied
      ? []
      : [
          {
            key: "inspect",
            title: "Inspect",
            Icon: Wrench,
            href: mcpServerTabHref(routes, idOrSlug, "inspect"),
            active: activeTab === "inspect",
          },
        ]),
    ...(canViewTeamAccess
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
    // Hidden for unproxied servers: the customer connects straight to the
    // vendor with the vendor's own credentials, so Gram never mints a session
    // or registers a client for them and the tab would always be empty.
    ...(isUnproxied
      ? []
      : [
          {
            key: "sessions",
            title: "Clients and Sessions",
            Icon: Plug,
            href: mcpServerTabHref(routes, idOrSlug, "sessions"),
            active: activeTab === "sessions",
          },
        ]),
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
        <Text className="truncate font-semibold">
          {mcpServer.name || "MCP Server"}
        </Text>
        {isRemoteBacked && (
          <DetailSidebarInfoLabel>Remote MCP</DetailSidebarInfoLabel>
        )}
        {isTunneledBacked && (
          <DetailSidebarInfoLabel>Tunneled MCP</DetailSidebarInfoLabel>
        )}
        {isUnproxied && (
          <DetailSidebarInfoLabel>Unproxied MCP</DetailSidebarInfoLabel>
        )}
      </div>

      <div className="flex flex-col gap-1.5">
        <DetailSidebarInfoLabel>Visibility</DetailSidebarInfoLabel>
        <MCPServerStatusDropdown server={mcpServer} />
      </div>

      {mcpUrl && (
        <div className="flex flex-col gap-1">
          <DetailSidebarInfoLabel>URL</DetailSidebarInfoLabel>
          <div className="flex items-start gap-1">
            <Text
              variant="small"
              muted
              className="line-clamp-2 font-mono text-xs break-all"
            >
              {mcpUrl.replace(/^https?:\/\//, "")}
            </Text>
            <CopyButton
              text={mcpUrl}
              size="xs"
              tooltip="Copy URL"
              className="mt-[-2px] shrink-0"
            />
          </div>
        </div>
      )}

      {upstreamUrl && (
        <div className="flex flex-col gap-1">
          <DetailSidebarInfoLabel>Upstream URL</DetailSidebarInfoLabel>
          <div className="flex items-start gap-1">
            <Text
              variant="small"
              muted
              className="line-clamp-2 font-mono text-xs break-all"
            >
              {upstreamUrl.replace(/^https?:\/\//, "")}
            </Text>
            <CopyButton
              text={upstreamUrl}
              size="xs"
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
        {isUnproxied ? (
          <span className="text-muted-foreground/50 flex flex-1 cursor-not-allowed items-center justify-center gap-1 text-xs font-semibold">
            Test in Playground
            <ArrowRight className="h-3 w-3" />
          </span>
        ) : (
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
        )}
      </div>
    </>
  );

  return (
    <DetailSidebarNav
      backHref={routes.mcp.href()}
      topTitle="Readiness"
      topContent={
        readinessChecks.length > 0 ? (
          <div className="flex flex-col gap-3">
            {/* The upstream endpoint is what the guide catalog indexes; a
                server with no upstream has no guide to point at. */}
            <SetupGuideCard serverUrl={upstreamUrl} />
            <McpServerReadinessBar checks={readinessChecks} />
          </div>
        ) : undefined
      }
      cardContent={cardContent}
      items={items}
    />
  );
}
