import { type ReadinessCheck } from "@/components/mcp-server-readiness-bar";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useSkillsOverMcpEnabled } from "@/pages/skills/use-skills-over-mcp";
import {
  gatewayInstallPageUrl,
  useResolvedMcpServerUrl,
} from "@/hooks/useToolsetUrl";
import {
  activeTabFromPath,
  gatewayTabHref,
  type TabValue,
} from "@/pages/mcp/gateway/GatewayDetailsRouting";
import { useGatewayMemberRows } from "@/pages/mcp/gateway/useGatewayMemberRows";
import { GATEWAY_AUTHENTICATION_SECTION_ID } from "@/pages/mcp/gateway/GatewaySettingsTab";
import { MCP_SERVER_URL_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/ServerUrlSection";
import { useRoutes } from "@/routes";
import { useGetMetaMcpServer } from "@gram/client/react-query/getMetaMcpServer.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { Network } from "lucide-react";
import { cn } from "@/lib/utils";
import type { McpDetailChrome } from "./mcpDetailChrome";
import { buildMcpDetailNavItems } from "./mcpDetailNavItems";
import { useLocation, useParams } from "react-router";

/**
 * Loads one gateway's detail chrome: its tabs, readiness checks and URLs, in
 * the same shape as an MCP server's so both pages share one header.
 */
export function useGatewayDetailChrome(): McpDetailChrome | null {
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
  const { mcpUrl } = useResolvedMcpServerUrl(endpoints, isLoadingEndpoints);
  const installPageUrl = gatewayInstallPageUrl(endpoints);
  const { rows } = useGatewayMemberRows(id);
  const { hasScope } = useRBAC();
  // Mirrors the mcp_servers sidebar: the tab reads org membership as well as
  // the resource, so hide it rather than render a permission wall.
  const canViewTeamAccess =
    !!metaMcpServer && hasScope("org:read") && hasScope("mcp:read", id);
  const project = useProject();
  // Same check as the Skills tab route: skill read on this project.
  const canViewSkills =
    useSkillsOverMcpEnabled() && hasScope("skill:read", project.id, project.id);
  if (!id || !metaMcpServer) return null;

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
          // Member management lives on the Overview tab.
          ready: rows.length > 0,
          href: gatewayTabHref(routes, id, "overview"),
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

  const items = buildMcpDetailNavItems<TabValue>({
    tabs: [
      "overview",
      "inspect",
      canViewSkills && "skills",
      canViewTeamAccess && "team-access",
      "sessions",
      "settings",
    ],
    activeTab,
    href: (tab) => gatewayTabHref(routes, id, tab),
  });

  return {
    name: metaMcpServer.name,
    icon: (className) => (
      <Network className={cn("text-muted-foreground", className)} />
    ),
    kindLabel: "Gateway",
    statusControl: null,
    primaryUrl: mcpUrl,
    readinessChecks,
    installLinks: installPageUrl
      ? [{ url: installPageUrl, label: "Install page" }]
      : [],
    items,
  };
}
