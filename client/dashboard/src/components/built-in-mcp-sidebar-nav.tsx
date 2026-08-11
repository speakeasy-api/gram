import {
  DetailSidebarInfoLabel,
  DetailSidebarNav,
  type DetailSidebarNavItem,
} from "@/components/detail/detail-sidebar-nav";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { useSlugs } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import {
  activeTabFromPath,
  builtInTabHref,
} from "@/pages/mcp/BuiltInMCPDetailRouting";
import { BUILT_IN_TOOLS } from "@/pages/mcp/builtInMcpTools";
import { useRoutes } from "@/routes";
import { LayoutDashboard, Wrench } from "lucide-react";
import * as React from "react";
import { useLocation, useParams } from "react-router";

export function BuiltInMcpSidebarNav(): React.JSX.Element | null {
  const routes = useRoutes();
  const location = useLocation();
  const { orgSlug } = useSlugs();
  const { builtInSlug } = useParams<{ builtInSlug: string }>();

  const idOrSlug = builtInSlug ?? "";
  if (!idOrSlug || !orgSlug) return null;

  const activeTab = activeTabFromPath(location.pathname, idOrSlug);
  const mcpUrl = `${getServerURL()}/mcp/${orgSlug}-mcp-logs`;

  const items: DetailSidebarNavItem[] = [
    {
      key: "overview",
      title: "Overview",
      Icon: LayoutDashboard,
      href: builtInTabHref(routes, idOrSlug, "overview"),
      active: activeTab === "overview",
    },
    {
      key: "tools",
      title: "Tools",
      Icon: Wrench,
      href: builtInTabHref(routes, idOrSlug, "tools"),
      active: activeTab === "tools",
    },
  ];

  const cardContent = (
    <>
      <div className="flex flex-col gap-0.5">
        <Text className="truncate font-semibold">MCP Logs</Text>
        <DetailSidebarInfoLabel>Built-in</DetailSidebarInfoLabel>
      </div>

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

      <div className="flex flex-col gap-1">
        <DetailSidebarInfoLabel>Tools</DetailSidebarInfoLabel>
        <Text variant="small">{BUILT_IN_TOOLS.length}</Text>
      </div>
    </>
  );

  return (
    <DetailSidebarNav
      backHref={routes.mcp.href()}
      cardContent={cardContent}
      items={items}
    />
  );
}
