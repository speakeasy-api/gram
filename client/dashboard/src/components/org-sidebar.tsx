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
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRBAC } from "@/hooks/useRBAC";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { AppRoute, useOrgRoutes } from "@/routes";
import { Icon } from "@speakeasy-api/moonshine";
import * as React from "react";
import { Link } from "react-router";
import { GramLogo } from "./gram-logo";
import { CommandPaletteTrigger } from "./command-palette/CommandPaletteTrigger";
import { SidebarNavSkeleton } from "./sidebar-nav-skeleton";
import { OnboardingResumeButton } from "./onboarding-resume-button";
import { SidebarUserMenu } from "./sidebar-user-menu";
import { WorkspaceSwitcher } from "./workspace-switcher";

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

export function OrgSidebar({
  ...props
}: React.ComponentProps<typeof Sidebar>): React.JSX.Element {
  const orgRoutes = useOrgRoutes();
  const { isRbacEnabled, isLoading: rbacLoading } = useRBAC();
  const telemetry = useTelemetry();
  const isDeviceAgentEnabled =
    telemetry.isFeatureEnabled("gram-device-agent") ?? false;
  const isUserSessionsEnabled =
    telemetry.isFeatureEnabled("user-sessions-dashboard") ?? false;

  const settingsActive = [
    orgRoutes.billing,
    orgRoutes.apiKeys,
    orgRoutes.domains,
    orgRoutes.logs,
    orgRoutes.webhooks,
  ].some((r) => r.active);

  const secureActive = [
    orgRoutes.auditLogs,
    orgRoutes.deviceAgent,
    orgRoutes.access,
  ].some((r) => r.active);

  const identityActive = [
    orgRoutes.userSessions,
    orgRoutes.identity,
    orgRoutes.remoteIdentityProviders,
  ].some((r) => r.active);

  const activeGroup = settingsActive
    ? "Settings"
    : secureActive
      ? "Secure"
      : identityActive
        ? "Identity"
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
    orgRoutes.auditLogs,
    orgRoutes.deviceAgent,
    orgRoutes.access,
    orgRoutes.userSessions,
    orgRoutes.identity,
    orgRoutes.remoteIdentityProviders,
  ];
  const activeRoute = allOrgNavRoutes.find((r) => r.active);
  const activeItem = activeRoute?.title;

  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarHeader className="gap-3 pb-3">
        <div className="flex items-center justify-between gap-2 group-data-[collapsible=icon]:justify-center">
          <Link
            to={orgRoutes.home.href()}
            className="flex h-(--header-height) items-center px-1 hover:no-underline group-data-[collapsible=icon]:hidden"
          >
            <GramLogo className="w-28" />
          </Link>
          <CommandPaletteTrigger />
        </div>
        <WorkspaceSwitcher />
      </SidebarHeader>
      <SidebarContent className="pt-2">
        {rbacLoading ? (
          <SidebarNavSkeleton />
        ) : (
          <NavGroupProvider
            activeGroup={activeGroup}
            defaultOpenGroups={["Settings", "Secure", "Identity"]}
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
                {isDeviceAgentEnabled && (
                  <ScopeGatedNavItem
                    item={orgRoutes.deviceAgent}
                    scope={["org:read", "org:admin"]}
                  />
                )}
                {isRbacEnabled && (
                  <ScopeGatedNavItem
                    item={orgRoutes.access}
                    scope={["org:read", "org:admin", "org:manage_roles"]}
                  />
                )}
              </CollapsibleNavGroup>

              {/* Identity group */}
              <CollapsibleNavGroup
                label="Identity"
                Icon={(p) => <Icon {...p} name="fingerprint" />}
                defaultHref={orgRoutes.identity.href()}
              >
                {isUserSessionsEnabled && (
                  <ScopeGatedNavItem
                    item={orgRoutes.userSessions}
                    scope={["org:read", "org:admin"]}
                  />
                )}
                <ScopeGatedNavItem
                  item={orgRoutes.identity}
                  scope={["org:read", "org:admin", "org:manage_roles"]}
                />
                <ScopeGatedNavItem
                  item={orgRoutes.remoteIdentityProviders}
                  scope={["org:read", "org:admin"]}
                />
              </CollapsibleNavGroup>
            </SidebarMenu>
          </NavGroupProvider>
        )}
      </SidebarContent>
      <SidebarFooter className="border-t">
        <OnboardingResumeButton />
        <SidebarUserMenu />
      </SidebarFooter>
    </Sidebar>
  );
}
