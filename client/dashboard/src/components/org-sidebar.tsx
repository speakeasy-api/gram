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
import { useIsAdmin } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { Scope, useRBAC } from "@/hooks/useRBAC";
import { AppRoute, useOrgRoutes } from "@/routes";
import { Icon } from "@speakeasy-api/moonshine";
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
  const isAdmin = useIsAdmin();
  const { isRbacEnabled } = useRBAC();
  const telemetry = useTelemetry();
  const isDeviceAgentEnabled =
    telemetry.isFeatureEnabled("gram-device-agent") ?? false;

  const settingsActive = [
    orgRoutes.billing,
    orgRoutes.apiKeys,
    orgRoutes.domains,
    orgRoutes.logs,
    orgRoutes.webhooks,
    orgRoutes.adminSettings,
  ].some((r) => r.active);

  const secureActive = [
    orgRoutes.auditLogs,
    orgRoutes.identity,
    orgRoutes.deviceAgent,
    orgRoutes.access,
  ].some((r) => r.active);

  const activeGroup = settingsActive
    ? "Settings"
    : secureActive
      ? "Secure"
      : undefined;

  const allOrgNavRoutes = [
    orgRoutes.home,
    orgRoutes.collections,
    orgRoutes.team,
    orgRoutes.billing,
    orgRoutes.apiKeys,
    orgRoutes.domains,
    orgRoutes.logs,
    orgRoutes.webhooks,
    orgRoutes.adminSettings,
    orgRoutes.auditLogs,
    orgRoutes.identity,
    orgRoutes.deviceAgent,
    orgRoutes.access,
  ];
  const activeRoute = allOrgNavRoutes.find((r) => r.active);
  const activeItem = activeRoute?.title;

  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarContent className="pt-5">
        <NavGroupProvider
          activeGroup={activeGroup}
          defaultOpenGroups={["Settings", "Secure"]}
          activeItem={activeItem}
        >
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

            {/* Team — top-level */}
            <ScopeGatedTopLevelItem
              item={orgRoutes.team}
              scope={["org:read", "org:admin"]}
            />

            {/* Settings group */}
            <CollapsibleNavGroup
              label="Settings"
              Icon={(p) => <Icon {...p} name="settings" />}
              defaultHref={orgRoutes.billing.href()}
            >
              <ScopeGatedNavItem
                item={orgRoutes.billing}
                scope={["org:read", "org:admin"]}
              />
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
              label="Secure"
              Icon={(p) => <Icon {...p} name="shield-check" />}
              defaultHref={orgRoutes.auditLogs.href()}
            >
              <ScopeGatedNavItem
                item={orgRoutes.auditLogs}
                scope={["org:read", "org:admin"]}
              />
              <ScopeGatedNavItem
                item={orgRoutes.identity}
                scope={["org:read", "org:admin"]}
              />
              {isDeviceAgentEnabled && (
                <ScopeGatedNavItem
                  item={orgRoutes.deviceAgent}
                  scope={["org:read", "org:admin"]}
                />
              )}
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
