import * as React from "react";
import { NavMenu } from "@/components/nav-menu";
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
import { Link, useNavigate } from "react-router-dom";
import { ROUTES } from "@/routes";

import { ProjectMenu } from "./project-menu";
import { cn } from "@/lib/utils";
import { GramLogo } from "./gram-logo";

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const navigate = useNavigate();

  // Reverse sort the items by url length to ensure the most specific item is selected
  const activeItem = [
    ROUTES.primaryCTA,
    ...ROUTES.navMain,
    ...ROUTES.navSecondary,
  ]
    .sort((a, b) => b.url.length - a.url.length)
    .find((item) => location.pathname.startsWith(item.url));

  const topNavItems = ROUTES.navMain.map((item) => ({
    ...item,
    active: activeItem?.url === item.url,
  }));

  const secondaryNavItems = ROUTES.navSecondary.map((item) => ({
    ...item,
    active: activeItem?.url === item.url,
  }));

  const primaryActive = activeItem?.url === ROUTES.primaryCTA.url;

  const primaryCTA = (
    <SidebarMenuButton
      tooltip={ROUTES.primaryCTA.title}
      className={cn(
        "bg-primary! text-primary-foreground! hover:bg-primary/90 hover:text-primary-foreground min-w-8 trans",
        primaryActive && "border-violet-300 border-2 scale-105",
      )}
      onClick={() => {
        navigate(ROUTES.primaryCTA.url);
      }}
      isActive={primaryActive}
    >
      <ROUTES.primaryCTA.icon />
      <span>{ROUTES.primaryCTA.title}</span>
    </SidebarMenuButton>
  );

  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem className="group/logo">
            <SidebarMenuButton
              asChild
              className="data-[slot=sidebar-menu-button]:!p-1.5 h-12"
            >
              <Link to="/">
                <GramLogo />
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent className="flex flex-col gap-6">
            <SidebarMenu>
              <SidebarMenuItem className="flex items-center gap-2">
                {primaryCTA}
              </SidebarMenuItem>
            </SidebarMenu>
            <NavMenu items={topNavItems} />
          </SidebarGroupContent>
        </SidebarGroup>
        <SidebarGroup className="mt-auto">
          <SidebarGroupContent>
            <NavMenu items={secondaryNavItems} />
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter>
        <ProjectMenu />
      </SidebarFooter>
    </Sidebar>
  );
}
