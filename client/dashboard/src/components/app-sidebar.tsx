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
import { useRoutes } from "@/routes";
import { ProjectMenu } from "./project-menu";
import { cn } from "@/lib/utils";
import { GramLogo } from "./gram-logo";

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const routes = useRoutes();

  const topNavItems = [
    routes.home,
    routes.integrations,
    routes.toolsets,
    routes.environments,
  ];

  const secondaryNavItems = [routes.sdk, routes.settings, routes.docs];

  const playgroundCTA = (
    <SidebarMenuButton
      tooltip={routes.playground.title}
      className={cn(
        "bg-primary! text-primary-foreground! hover:bg-primary/90 hover:text-primary-foreground min-w-8 trans",
        routes.playground.active && "border-violet-300 border-2 scale-105"
      )}
      onClick={() => {
        routes.playground.goTo();
      }}
      isActive={routes.playground.active}
    >
      <routes.playground.Icon />
      <span>{routes.playground.title}</span>
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
              <routes.home.Link>
                <GramLogo />
              </routes.home.Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent className="flex flex-col gap-6">
            <SidebarMenu>
              <SidebarMenuItem className="flex items-center gap-2">
                {playgroundCTA}
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
