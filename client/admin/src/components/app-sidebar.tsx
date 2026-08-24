import type { ComponentProps, JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { BuildingIcon, FolderIcon } from "lucide-react";
import { Link, useLocation, useMatchRoute } from "@tanstack/react-router";

import { NavUser } from "@/components/nav-user";
import { RecordNav } from "@/components/record-nav";
import { SpeakeasyMark } from "@/components/speakeasy-mark";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { organizationQuery } from "@/lib/adminQueries";

// `as const` keeps each `to` a literal, which is what the router types check
// the link against.
const navItems = [
  { to: "/organizations", label: "Organizations", icon: BuildingIcon },
  { to: "/projects", label: "Projects", icon: FolderIcon },
] as const;

export function AppSidebar({
  ...props
}: ComponentProps<typeof Sidebar>): JSX.Element {
  const { pathname } = useLocation();
  const matchRoute = useMatchRoute();
  const record = matchRoute({ to: "/organizations/$idOrSlug", fuzzy: true });
  const idOrSlug = record ? record.idOrSlug : "";

  // The same query the record layout reads, so it costs no second request and
  // the two cannot disagree about which record this is. The sidebar has to ask:
  // a contextual nav over a record that never arrived leaves the operator a
  // back link and nothing else. `RecordLayout` branches on this same `data`, so
  // a record held through a failed refetch keeps its nav as well as its views.
  const { data: org } = useQuery({
    ...organizationQuery(idOrSlug),
    enabled: !!idOrSlug,
  });

  return (
    <Sidebar collapsible="icon" variant="inset" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              asChild
              className="data-[slot=sidebar-menu-button]:!p-1.5"
            >
              <Link to="/">
                <SpeakeasyMark className="!size-5 group-data-[collapsible=icon]:!size-4" />
                <span className="text-base font-semibold">
                  AI Control Plane Admin
                </span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
        {org ? (
          <RecordNav idOrSlug={idOrSlug} org={org} />
        ) : (
          <SidebarGroup>
            <SidebarGroupContent>
              <SidebarMenu>
                {navItems.map(({ to, label, icon: Icon }) => (
                  <SidebarMenuItem key={to}>
                    <SidebarMenuButton
                      asChild
                      isActive={pathname.startsWith(to)}
                      tooltip={label}
                    >
                      <Link to={to}>
                        <Icon />
                        <span>{label}</span>
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        )}
      </SidebarContent>

      <SidebarFooter>
        <NavUser />
      </SidebarFooter>
    </Sidebar>
  );
}
