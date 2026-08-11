import type { ComponentProps, JSX } from "react";
import { BuildingIcon, FolderIcon } from "lucide-react";
import { Link, useLocation } from "@tanstack/react-router";

import { NavUser } from "@/components/nav-user";
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
                <span className="text-base font-semibold">AICP Admin</span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
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
      </SidebarContent>

      <SidebarFooter>
        <NavUser />
      </SidebarFooter>
    </Sidebar>
  );
}
