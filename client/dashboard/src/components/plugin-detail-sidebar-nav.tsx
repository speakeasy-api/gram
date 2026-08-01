import { MemberFacepile } from "@/components/member-facepile";
import {
  McpSidebarInfoLabel,
  McpSidebarNavShell,
  type McpSidebarNavItem,
} from "@/components/mcp-sidebar-nav-shell";
import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";
import {
  PLUGIN_ASSIGNMENTS_SECTION_ID,
  PLUGIN_OVERVIEW_SECTION_ID,
  PLUGIN_SERVERS_SECTION_ID,
  PLUGIN_SETTINGS_SECTION_ID,
  PLUGIN_SKILLS_SECTION_ID,
} from "@/pages/plugins/plugin-detail-sections";
import {
  individualMemberFacepile,
  memberMapByUrn,
} from "@/pages/plugins/principals";
import { usePluginAssignmentsVisible } from "@/pages/plugins/use-plugin-assignments-visible";
import { useRoutes } from "@/routes";
import { useMembers } from "@gram/client/react-query/members";
import { usePlugin } from "@gram/client/react-query/plugin";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import {
  LayoutDashboard,
  Network,
  Settings as SettingsIcon,
  Sparkles,
  Users,
} from "lucide-react";
import * as React from "react";
import { useLocation, useParams } from "react-router";

export function PluginDetailSidebarNav(): React.JSX.Element | null {
  const routes = useRoutes();
  const location = useLocation();
  const { pluginId } = useParams<{ pluginId: string }>();

  const { data: plugin } = usePlugin({ id: pluginId ?? "" }, undefined, {
    throwOnError: false,
    enabled: !!pluginId,
  });
  const { data: publishStatus } = usePublishStatus();
  const { data: membersData } = useMembers();
  const showAssignments = usePluginAssignmentsVisible();

  const memberByUrn = React.useMemo(
    () => memberMapByUrn(membersData?.members ?? []),
    [membersData?.members],
  );

  if (!pluginId) return null;

  const detailHref = routes.plugins.detail.href(pluginId);
  const activeSectionId = location.hash.replace("#", "");
  const sectionItem = (
    sectionId: string,
    title: string,
    Icon: React.ComponentType<{ className?: string }>,
    isDefault = false,
  ): McpSidebarNavItem => ({
    key: sectionId,
    title,
    Icon,
    href: `${detailHref}#${sectionId}`,
    active:
      activeSectionId === sectionId || (isDefault && activeSectionId === ""),
  });

  const items: McpSidebarNavItem[] = [
    sectionItem(PLUGIN_OVERVIEW_SECTION_ID, "Overview", LayoutDashboard, true),
    sectionItem(PLUGIN_SERVERS_SECTION_ID, "MCP Servers", Network),
    sectionItem(PLUGIN_SKILLS_SECTION_ID, "Skills", Sparkles),
    ...(showAssignments
      ? [sectionItem(PLUGIN_ASSIGNMENTS_SECTION_ID, "Assignments", Users)]
      : []),
    sectionItem(PLUGIN_SETTINGS_SECTION_ID, "Settings", SettingsIcon),
  ];

  const assignments = plugin?.assignments ?? [];
  const facepileMembers = individualMemberFacepile(assignments, memberByUrn);
  const cleanVersion = publishStatus?.connected
    ? publishStatus.liveVersion?.replace(/[^\w.\-+]/g, "").trim()
    : undefined;
  // Only the explicit booleans are a known freshness — an undefined upToDate
  // (connection predates fingerprinting) must not read as "Up to date".
  const hasUnpublishedChanges = publishStatus?.upToDate === false;
  const isUpToDate = publishStatus?.upToDate === true;
  const showMarketplaceInfo =
    !!publishStatus?.connected &&
    (hasUnpublishedChanges || isUpToDate || !!cleanVersion);
  const serverCount = plugin?.serverCount ?? plugin?.servers?.length ?? 0;

  const cardContent = plugin && (
    <>
      <div className="flex flex-col gap-0.5">
        <div className="flex items-center gap-1.5">
          <Text className="truncate font-semibold">{plugin.name}</Text>
          {plugin.isDefault && (
            <Badge variant="information">
              <Badge.Text>Default</Badge.Text>
            </Badge>
          )}
        </div>
        <Text variant="small" muted className="truncate font-mono text-xs">
          {plugin.slug}
        </Text>
      </div>

      {showMarketplaceInfo && (
        <div className="flex flex-col gap-1">
          <McpSidebarInfoLabel>Marketplace</McpSidebarInfoLabel>
          <div className="flex flex-wrap items-center gap-1.5">
            {hasUnpublishedChanges && (
              <Badge variant="warning">
                <Badge.Text>Needs syncing</Badge.Text>
              </Badge>
            )}
            {isUpToDate && (
              <Badge variant="success">
                <Badge.Text>Up to date</Badge.Text>
              </Badge>
            )}
            {cleanVersion && (
              <Badge variant="information">
                <Badge.Text className="font-mono">{cleanVersion}</Badge.Text>
              </Badge>
            )}
          </div>
        </div>
      )}

      <div className="flex gap-6">
        <div className="flex flex-col gap-1">
          <McpSidebarInfoLabel>Servers</McpSidebarInfoLabel>
          <Text variant="small">{serverCount}</Text>
        </div>
        <div className="flex flex-col gap-1">
          <McpSidebarInfoLabel>Skills</McpSidebarInfoLabel>
          <Text variant="small">{plugin.skillCount ?? 0}</Text>
        </div>
      </div>

      {showAssignments && (
        <div className="flex flex-col gap-1.5">
          <McpSidebarInfoLabel>Distributed to</McpSidebarInfoLabel>
          {facepileMembers.length > 0 ? (
            <div className="flex items-center gap-2">
              <MemberFacepile members={facepileMembers} />
              <Text variant="small" muted className="text-xs">
                {facepileMembers.length}{" "}
                {facepileMembers.length === 1 ? "member" : "members"}
              </Text>
            </div>
          ) : (
            <Text variant="small" muted className="text-xs">
              {assignments.length > 0
                ? `${assignments.length} ${assignments.length === 1 ? "assignment" : "assignments"}`
                : "No one yet"}
            </Text>
          )}
        </div>
      )}
    </>
  );

  return (
    <McpSidebarNavShell
      backHref={routes.plugins.href()}
      backLabel="Back to all plugins"
      cardContent={cardContent}
      items={items}
      itemsTitle="Sections"
    />
  );
}
