import { NavButton, NavMenu } from "@/components/nav-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { useIsAdmin, useOrganization } from "@/contexts/Auth";
import { useOrgRoutes } from "@/routes";
import { Icon } from "@speakeasy-api/moonshine";
import { ExternalLink } from "lucide-react";
import * as React from "react";

export function OrgSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const orgRoutes = useOrgRoutes();
  const organization = useOrganization();
  const isAdmin = useIsAdmin();

  const teamUrl =
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
            <NavMenu items={[orgRoutes.home]} />
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarGroup>
          <SidebarGroupLabel>settings</SidebarGroupLabel>
          <SidebarGroupContent>
            <NavMenu
              items={[
                orgRoutes.billing,
                orgRoutes.apiKeys,
                orgRoutes.domains,
                orgRoutes.logs,
                orgRoutes.auditLogs,
                ...(isAdmin ? [orgRoutes.adminSettings] : []),
              ]}
            >
              <SidebarMenuItem>
                <NavButton
                  title="Team"
                  titleNode={
                    <span className="flex items-center gap-1.5">
                      Team
                      <ExternalLink className="w-3 h-3 text-muted-foreground" />
                    </span>
                  }
                  href={teamUrl}
                  target="_blank"
                  Icon={(props) => <Icon name="users-round" {...props} />}
                />
              </SidebarMenuItem>
            </NavMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
    </Sidebar>
  );
}
