import type { ComponentProps, JSX } from "react";
import {
  BuildingIcon,
  CommandIcon,
  FolderIcon,
  type LucideIcon,
} from "lucide-react";
import { NavLink, useLocation } from "react-router";

import { NavUser } from "@/components/nav-user";
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

const navItems: { to: string; label: string; icon: LucideIcon }[] = [
  { to: "/organizations", label: "Organizations", icon: BuildingIcon },
  { to: "/projects", label: "Projects", icon: FolderIcon },
];

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
              <NavLink to="/">
                <CommandIcon className="!size-5 group-data-[collapsible=icon]:!size-4" />
                <span className="text-base font-semibold">Gram Admin</span>
              </NavLink>
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
                    <NavLink to={to}>
                      <Icon />
                      <span>{label}</span>
                    </NavLink>
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
