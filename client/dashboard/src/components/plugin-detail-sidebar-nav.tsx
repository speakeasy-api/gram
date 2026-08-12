import { MemberFacepile } from "@/components/member-facepile";
import {
  DetailSidebarInfoLabel,
  DetailSidebarNav,
  type DetailSidebarNavItem,
} from "@/components/detail/detail-sidebar-nav";
import { Badge } from "@/components/ui/Badge";
import { Text } from "@/components/ui/Text";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import {
  activePluginSection,
  pluginSectionHref,
  PLUGIN_ASSIGNMENTS_SECTION_ID,
  PLUGIN_OVERVIEW_SECTION_ID,
  PLUGIN_SERVERS_SECTION_ID,
  PLUGIN_SETTINGS_SECTION_ID,
  PLUGIN_SKILLS_SECTION_ID,
  type PluginSection,
} from "@/pages/plugins/plugin-detail-sections";
import {
  describePrincipal,
  individualMemberFacepile,
  isIndividualMemberPrincipal,
  memberMapByUrn,
  principalIcon,
  roleMapByUrn,
} from "@/pages/plugins/principals";
import { usePluginAssignmentsVisible } from "@/pages/plugins/use-plugin-assignments-visible";
import { useRoutes } from "@/routes";
import { useMembers } from "@gram/client/react-query/members";
import { usePlugin } from "@gram/client/react-query/plugin";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import { useRoles } from "@gram/client/react-query/roles";
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
  const { data: rolesData } = useRoles();
  const showAssignments = usePluginAssignmentsVisible();

  const memberByUrn = React.useMemo(
    () => memberMapByUrn(membersData?.members ?? []),
    [membersData?.members],
  );
  const roleByUrn = React.useMemo(
    () => roleMapByUrn(rolesData?.roles ?? []),
    [rolesData?.roles],
  );

  if (!pluginId) return null;

  const activeSectionId = activePluginSection(location.pathname, pluginId);
  const sectionItem = (
    sectionId: PluginSection,
    title: string,
    Icon: React.ComponentType<{ className?: string }>,
  ): DetailSidebarNavItem => ({
    key: sectionId,
    title,
    Icon,
    href: pluginSectionHref(routes, pluginId, sectionId),
    active: activeSectionId === sectionId,
  });

  const items: DetailSidebarNavItem[] = [
    sectionItem(PLUGIN_OVERVIEW_SECTION_ID, "Overview", LayoutDashboard),
    sectionItem(PLUGIN_SERVERS_SECTION_ID, "MCP Servers", Network),
    sectionItem(PLUGIN_SKILLS_SECTION_ID, "Skills", Sparkles),
    ...(showAssignments
      ? [sectionItem(PLUGIN_ASSIGNMENTS_SECTION_ID, "Assignments", Users)]
      : []),
    sectionItem(PLUGIN_SETTINGS_SECTION_ID, "Settings", SettingsIcon),
  ];

  const assignments = plugin?.assignments ?? [];
  const facepileMembers = individualMemberFacepile(assignments, memberByUrn);
  // Everyone/role/email principals have no avatar, so they sit beside the
  // face-stack as icon chips rather than being dropped from the glance.
  const nonMemberAssignments = assignments.filter(
    (a) => !isIndividualMemberPrincipal(a.principalUrn),
  );
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
          <DetailSidebarInfoLabel>Marketplace</DetailSidebarInfoLabel>
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
          <DetailSidebarInfoLabel>Servers</DetailSidebarInfoLabel>
          <Text variant="small">{serverCount}</Text>
        </div>
        <div className="flex flex-col gap-1">
          <DetailSidebarInfoLabel>Skills</DetailSidebarInfoLabel>
          <Text variant="small">{plugin.skillCount ?? 0}</Text>
        </div>
      </div>

      {showAssignments && (
        <div className="flex flex-col gap-1.5">
          <DetailSidebarInfoLabel>Distributed to</DetailSidebarInfoLabel>
          {assignments.length === 0 ? (
            <Text variant="small" muted className="text-xs">
              No one yet
            </Text>
          ) : (
            <div className="flex flex-wrap items-center gap-1.5">
              {facepileMembers.length > 0 && (
                <MemberFacepile members={facepileMembers} />
              )}
              {nonMemberAssignments.map((assignment) => {
                const { kind, label } = describePrincipal(
                  assignment.principalUrn,
                  roleByUrn,
                  memberByUrn,
                );
                const PrincipalIcon = principalIcon(kind);
                return (
                  <Tooltip key={assignment.id}>
                    <TooltipTrigger asChild>
                      <div
                        role="img"
                        aria-label={label}
                        tabIndex={0}
                        className="bg-muted text-muted-foreground ring-background focus-visible:ring-ring flex size-7 shrink-0 items-center justify-center rounded-full ring-2 focus-visible:outline-none"
                      >
                        <PrincipalIcon className="size-3.5" />
                      </div>
                    </TooltipTrigger>
                    <TooltipContent side="top">{label}</TooltipContent>
                  </Tooltip>
                );
              })}
            </div>
          )}
        </div>
      )}
    </>
  );

  return (
    <DetailSidebarNav
      backHref={routes.plugins.href()}
      backLabel="Back to all plugins"
      cardContent={cardContent}
      items={items}
    />
  );
}
