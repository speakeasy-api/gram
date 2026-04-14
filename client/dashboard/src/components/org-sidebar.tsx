import { NavButton } from "@/components/nav-menu";
import { RequireScope } from "@/components/require-scope";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { useIsAdmin, useOrganization } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { Scope } from "@/hooks/useRBAC";
import { AppRoute, useOrgRoutes } from "@/routes";
import { Icon } from "@speakeasy-api/moonshine";
import { ExternalLink } from "lucide-react";
import * as React from "react";

/**
 * Nav items use hasAnyScope — the link is enabled if the user holds ANY of the
 * provided scopes (view OR mutate). Fine-grained enforcement happens inside
 * each page via page-level and component-level RequireScope guards.
 */
function ScopeGatedNavItem({
  item,
  scope,
}: {
  item: AppRoute;
  scope: Scope | Scope[];
}) {
  return (
    <SidebarMenuItem>
      <RequireScope scope={scope} level="component">
        <NavButton
          title={item.title}
          href={item.href()}
          active={item.active}
          Icon={item.Icon}
        />
      </RequireScope>
    </SidebarMenuItem>
  );
}

export function OrgSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const orgRoutes = useOrgRoutes();
  const organization = useOrganization();
  const isAdmin = useIsAdmin();
  const telemetry = useTelemetry();
  const isRbacEnabled = telemetry.isFeatureEnabled("gram-rbac") ?? false;
  const isTeamPageEnabled =
    telemetry.isFeatureEnabled("gram-team-page") ?? false;

  const externalTeamUrl =
    organization?.userWorkspaceSlugs &&
    organization.userWorkspaceSlugs.length > 0
      ? `https://app.speakeasy.com/org/${organization.slug}/${organization.userWorkspaceSlugs[0]}/settings/team`
      : "https://app.speakeasy.com";

  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarContent className="pt-2">
        <SidebarGroup>
          <SidebarGroupLabel>projects</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <ScopeGatedNavItem
                item={orgRoutes.home}
                scope={["build:read", "org:admin"]}
              />
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarGroup>
          <SidebarGroupLabel>settings</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
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
                <SidebarMenuItem>
                  <RequireScope
                    scope={["org:read", "org:admin"]}
                    level="component"
                  >
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
                  </RequireScope>
                </SidebarMenuItem>
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
                item={orgRoutes.auditLogs}
                scope={["org:read", "org:admin"]}
              />
              {isRbacEnabled && (
                <ScopeGatedNavItem
                  item={orgRoutes.access}
                  scope={["org:read", "org:admin"]}
                />
              )}
              {isAdmin && (
                <SidebarMenuItem>
                  <NavButton
                    title={orgRoutes.adminSettings.title}
                    href={orgRoutes.adminSettings.href()}
                    active={orgRoutes.adminSettings.active}
                    Icon={orgRoutes.adminSettings.Icon}
                  />
                </SidebarMenuItem>
              )}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
    </Sidebar>
  );
}
