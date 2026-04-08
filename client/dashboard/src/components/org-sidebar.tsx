import { NavMenu } from "@/components/nav-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
} from "@/components/ui/sidebar";
import { useIsAdmin, useOrganization } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { useOrgRoutes } from "@/routes";
import * as React from "react";

export function OrgSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const orgRoutes = useOrgRoutes();
  const isAdmin = useIsAdmin();
  const telemetry = useTelemetry();
  const isRbacEnabled = telemetry.isFeatureEnabled("gram-rbac") ?? false;

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
                orgRoutes.team,
                orgRoutes.apiKeys,
                orgRoutes.domains,
                orgRoutes.logs,
                orgRoutes.auditLogs,
                ...(isRbacEnabled ? [orgRoutes.access] : []),
                ...(isAdmin ? [orgRoutes.adminSettings] : []),
              ]}
            />
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
    </Sidebar>
  );
}
