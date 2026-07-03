import {
  McpSidebarInfoLabel,
  McpSidebarNavShell,
  type McpSidebarNavItem,
} from "@/components/mcp-sidebar-nav-shell";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { getMcpServerArgs } from "@/lib/sources";
import { MCPServerStatusDropdown } from "@/pages/mcp/x/MCPServerDetails";
import {
  activeTabFromPath,
  mcpServerTabHref,
} from "@/pages/mcp/x/MCPServerDetailsRouting";
import { useRoutes } from "@/routes";
import { useGetMcpServer } from "@gram/client/react-query/index.js";
import {
  BarChart3,
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

  if (!idOrSlug) return null;

  const activeTab = activeTabFromPath(location.pathname, idOrSlug);
  const isRemoteBacked = !!mcpServer?.remoteMcpServerId;

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
    {
      key: "analytics",
      title: "Analytics",
      Icon: BarChart3,
      href: mcpServerTabHref(routes, idOrSlug, "analytics"),
      active: activeTab === "analytics",
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
      </div>

      <div className="flex flex-col gap-1.5">
        <McpSidebarInfoLabel>Visibility</McpSidebarInfoLabel>
        <MCPServerStatusDropdown server={mcpServer} />
      </div>
    </>
  );

  return (
    <McpSidebarNavShell
      backHref={routes.mcp.href()}
      cardContent={cardContent}
      items={items}
    />
  );
}
