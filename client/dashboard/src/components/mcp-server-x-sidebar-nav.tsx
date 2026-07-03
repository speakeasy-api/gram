import {
  McpSidebarInfoLabel,
  McpSidebarNavShell,
  type McpSidebarNavItem,
} from "@/components/mcp-sidebar-nav-shell";
import { CopyButton } from "@/components/ui/copy-button";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { getMcpServerArgs } from "@/lib/sources";
import { useResolvedMcpServerUrl } from "@/hooks/useToolsetUrl";
import { MCPServerStatusDropdown } from "@/pages/mcp/x/MCPServerDetails";
import {
  activeTabFromPath,
  mcpServerTabHref,
} from "@/pages/mcp/x/MCPServerDetailsRouting";
import { useRoutes } from "@/routes";
import {
  useGetMcpServer,
  useMcpEndpoints,
} from "@gram/client/react-query/index.js";
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

      <div className="flex flex-col gap-1">
        <McpSidebarInfoLabel>Endpoints</McpSidebarInfoLabel>
        <Type variant="small">{endpoints.length}</Type>
      </div>

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
        <div className="bg-border w-px self-stretch" />
        <routes.playground.Link className="flex flex-1 items-center justify-center hover:no-underline">
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
      cardContent={cardContent}
      items={items}
    />
  );
}
