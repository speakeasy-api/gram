import {
  CollapsibleNavGroup,
  CollapsibleNavItem,
  NavButton,
  NavGroupProvider,
} from "@/components/nav-menu";
import { RequireScope } from "@/components/require-scope";
import {
  Sidebar,
  SidebarContent,
  SidebarMenu,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { useIsAdmin, useOrganization } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { Scope, useRBAC } from "@/hooks/useRBAC";
import { AppRoute, useOrgRoutes } from "@/routes";
import { Icon } from "@speakeasy-api/moonshine";
import { ExternalLink } from "lucide-react";
import * as React from "react";

function ScopeGatedNavItem({
  item,
  scope,
}: {
  item: AppRoute;
  scope: Scope | Scope[];
}) {
  return (
    <RequireScope scope={scope} level="section">
      <CollapsibleNavItem item={item} />
    </RequireScope>
  );
}

function ScopeGatedTopLevelItem({
  item,
  scope,
}: {
  item: AppRoute;
  scope: Scope | Scope[];
}) {
  return (
    <RequireScope scope={scope} level="section">
      <SidebarMenuItem>
        <NavButton
          title={item.title}
          href={item.href()}
          active={item.active}
          Icon={item.Icon}
        />
      </SidebarMenuItem>
    </RequireScope>
  );
}

export function OrgSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const orgRoutes = useOrgRoutes();
  const organization = useOrganization();
  const isAdmin = useIsAdmin();
  const telemetry = useTelemetry();
  const { isRbacEnabled } = useRBAC();
  const isTeamPageEnabled =
    telemetry.isFeatureEnabled("gram-team-page") ?? false;

  const externalTeamUrl =
    organization?.userWorkspaceSlugs &&
    organization.userWorkspaceSlugs.length > 0
      ? `https://app.speakeasy.com/org/${organization.slug}/${organization.userWorkspaceSlugs[0]}/settings/team`
      : "https://app.speakeasy.com";

  const settingsActive = [
    orgRoutes.billing,
    orgRoutes.team,
    orgRoutes.apiKeys,
    orgRoutes.domains,
    orgRoutes.logs,
    orgRoutes.webhooks,
    orgRoutes.adminSettings,
  ].some((r) => r.active);

  const secureActive = [
    orgRoutes.auditLogs,
    orgRoutes.identity,
    orgRoutes.access,
  ].some((r) => r.active);

  const activeGroup = settingsActive
    ? "Settings"
    : secureActive
      ? "Security"
      : undefined;

  const allOrgNavRoutes = [
    orgRoutes.home,
    orgRoutes.collections,
    orgRoutes.billing,
    orgRoutes.team,
    orgRoutes.apiKeys,
    orgRoutes.domains,
    orgRoutes.logs,
    orgRoutes.webhooks,
    orgRoutes.adminSettings,
    orgRoutes.auditLogs,
    orgRoutes.identity,
    orgRoutes.access,
  ];
  const activeRoute = allOrgNavRoutes.find((r) => r.active);
  const activeItem = activeRoute?.title;

  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarContent className="pt-5">
        <NavGroupProvider activeGroup={activeGroup} activeItem={activeItem}>
          <SidebarMenu className="gap-1 px-2">
            {/* Home — top-level */}
            <ScopeGatedTopLevelItem
              item={orgRoutes.home}
              scope={["org:read", "project:read", "org:admin"]}
            />

            {/* Collections — top-level */}
            <ScopeGatedTopLevelItem
              item={orgRoutes.collections}
              scope={["org:read", "org:admin"]}
            />

            {/* Settings group */}
            <CollapsibleNavGroup
              label="Settings"
              Icon={(p) => <Icon {...p} name="settings" />}
              defaultHref={orgRoutes.billing.href()}
              isActive={settingsActive}
            >
              <ScopeGatedNavItem
                item={orgRoutes.billing}
                scope={["org:read", "org:admin"]}
              />
              {isTeamPageEnabled ? (
                <ScopeGatedNavItem
                  item={orgRoutes.team}
                  scope={["org:read", "org:admin"]}
                />
              ) : (
                <RequireScope
                  scope={["org:read", "org:admin"]}
                  level="component"
                  className="w-full"
                >
                  <li data-sidebar="menu-item">
                    <NavButton
                      title="Team"
                      titleNode={
                        <span className="flex items-center gap-1.5">
                          Team
                          <ExternalLink className="text-muted-foreground h-3 w-3" />
                        </span>
                      }
                      href={externalTeamUrl}
                      target="_blank"
                      Icon={(props) => <Icon name="users-round" {...props} />}
                    />
                  </li>
                </RequireScope>
              )}
              <ScopeGatedNavItem item={orgRoutes.apiKeys} scope="org:admin" />
              <ScopeGatedNavItem
                item={orgRoutes.domains}
                scope={["org:read", "org:admin"]}
              />
              <ScopeGatedNavItem
                item={orgRoutes.logs}
                scope={["org:read", "org:admin"]}
              />
              <ScopeGatedNavItem
                item={orgRoutes.webhooks}
                scope={["org:read", "org:admin"]}
              />
              {isAdmin && <CollapsibleNavItem item={orgRoutes.adminSettings} />}
            </CollapsibleNavGroup>

            {/* Secure group */}
            <CollapsibleNavGroup
              label="Security"
              Icon={(p) => <Icon {...p} name="shield-check" />}
              defaultHref={orgRoutes.auditLogs.href()}
              isActive={secureActive}
            >
              <ScopeGatedNavItem
                item={orgRoutes.auditLogs}
                scope={["org:read", "org:admin"]}
              />
              <ScopeGatedNavItem
                item={orgRoutes.identity}
                scope={["org:read", "org:admin"]}
              />
              {isRbacEnabled && (
                <ScopeGatedNavItem
                  item={orgRoutes.access}
                  scope={["org:read", "org:admin"]}
                />
              )}
            </CollapsibleNavGroup>
          </SidebarMenu>
        </NavGroupProvider>
      </SidebarContent>
    </Sidebar>
  );
}
