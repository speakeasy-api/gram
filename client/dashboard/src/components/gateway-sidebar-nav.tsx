import {
  DetailSidebarInfoLabel,
  DetailSidebarNav,
  type DetailSidebarNavItem,
} from "@/components/detail/detail-sidebar-nav";
import {
  McpServerReadinessBar,
  type ReadinessCheck,
} from "@/components/mcp-server-readiness-bar";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import {
  activeTabFromPath,
  gatewayTabHref,
} from "@/pages/mcp/gateway/GatewayDetailsRouting";
import { useGatewayMemberRows } from "@/pages/mcp/gateway/useGatewayMemberRows";
import { GATEWAY_AUTHENTICATION_SECTION_ID } from "@/pages/mcp/gateway/GatewaySettingsTab";
import { MCP_SERVER_URL_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/ServerUrlSection";
import { useRoutes } from "@/routes";
import { useGetMetaMcpServer } from "@gram/client/react-query/getMetaMcpServer.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import {
  ExternalLink,
  LayoutDashboard,
  Network,
  Plug,
  Settings as SettingsIcon,
  Users,
  Wrench,
} from "lucide-react";
import * as React from "react";
import { useLocation, useParams } from "react-router";

export function GatewaySidebarNav(): React.JSX.Element | null {
  const routes = useRoutes();
  const location = useLocation();
  const { gatewayId } = useParams<{ gatewayId: string }>();

  const id = gatewayId ?? "";
  const { data: metaMcpServer } = useGetMetaMcpServer({ id }, undefined, {
    enabled: id !== "",
  });
  const { data: endpointsResult, isLoading: isLoadingEndpoints } =
    useMcpEndpoints({ metaMcpServerId: id }, undefined, {
      enabled: id !== "",
    });
  const endpoints = endpointsResult?.mcpEndpoints ?? [];
  const { mcpUrl, installPageUrl } = useResolvedMcpServerUrl(
    endpoints,
    isLoadingEndpoints,
  );
  const { rows } = useGatewayMemberRows(id);
  const { hasScope } = useRBAC();
  // Mirrors the mcp_servers sidebar: the tab reads org membership as well as
  // the resource, so hide it rather than render a permission wall.
  const canViewTeamAccess =
    !!metaMcpServer && hasScope("org:read") && hasScope("mcp:read", id);
  if (!id) return null;

  const activeTab = activeTabFromPath(location.pathname, id);

  const readinessChecks: ReadinessCheck[] = metaMcpServer
    ? [
        {
          key: "server-url",
          label: "Gateway URL",
          description: mcpUrl
            ? "Endpoint is live and ready to connect to."
            : "Add an endpoint so this gateway has a URL to connect to.",
          ready: !!mcpUrl,
          href: `${gatewayTabHref(routes, id, "settings")}#${MCP_SERVER_URL_SECTION_ID}`,
        },
        {
          key: "members",
          label: "Members",
          description:
            rows.length > 0
              ? `Fronting ${rows.length} MCP ${rows.length === 1 ? "server" : "servers"}.`
              : "Add MCP servers for this gateway to front.",
          ready: rows.length > 0,
          href: gatewayTabHref(routes, id, "members"),
        },
        {
          key: "authentication",
          label: "Authentication",
          description: metaMcpServer.userSessionIssuerId
            ? "User sessions are configured for this gateway."
            : "Without an issuer the gateway serves anonymously.",
          ready: !!metaMcpServer.userSessionIssuerId,
          href: `${gatewayTabHref(routes, id, "settings")}#${GATEWAY_AUTHENTICATION_SECTION_ID}`,
        },
      ]
    : [];

  const items: DetailSidebarNavItem[] = [
    {
      key: "overview",
      title: "Overview",
      Icon: LayoutDashboard,
      href: gatewayTabHref(routes, id, "overview"),
      active: activeTab === "overview",
    },
    {
      key: "members",
      title: "Members",
      Icon: Network,
      href: gatewayTabHref(routes, id, "members"),
      active: activeTab === "members",
    },
    {
      key: "inspect",
      title: "Inspect",
      Icon: Wrench,
      href: gatewayTabHref(routes, id, "inspect"),
      active: activeTab === "inspect",
    },
    ...(canViewTeamAccess
      ? [
          {
            key: "team-access",
            title: "Team Access",
            Icon: Users,
            href: gatewayTabHref(routes, id, "team-access"),
            active: activeTab === "team-access",
          },
        ]
      : []),
    {
      key: "sessions",
      title: "Clients and Sessions",
      Icon: Plug,
      href: gatewayTabHref(routes, id, "sessions"),
      active: activeTab === "sessions",
    },
    {
      key: "settings",
      title: "Settings",
      Icon: SettingsIcon,
      href: gatewayTabHref(routes, id, "settings"),
      active: activeTab === "settings",
    },
  ];

  const cardContent = metaMcpServer && (
    <>
      <div className="flex items-center gap-2.5">
        <Network className="text-muted-foreground h-6 w-6 shrink-0" />
        <div className="flex min-w-0 flex-col gap-0.5">
          <Text className="truncate font-semibold">{metaMcpServer.name}</Text>
          <DetailSidebarInfoLabel>Gateway</DetailSidebarInfoLabel>
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <DetailSidebarInfoLabel>Members</DetailSidebarInfoLabel>
        <Text variant="small" muted>
          {`${rows.length} MCP ${rows.length === 1 ? "server" : "servers"}`}
        </Text>
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

      {/* Only the install page is offered: the playground can't connect to a
          gateway endpoint yet, and a permanently disabled control reads as
          broken rather than forthcoming. */}
      {installPageUrl && (
        <div className="border-border flex items-stretch border-t pt-3">
          <a
            href={installPageUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-muted-foreground hover:text-foreground flex flex-1 items-center justify-center gap-1 text-xs font-semibold transition-colors hover:no-underline"
          >
            Installation page
            <ExternalLink className="h-3 w-3" />
          </a>
        </div>
      )}
    </>
  );

  return (
    <DetailSidebarNav
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
