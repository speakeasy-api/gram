import { NavMenu } from "@/components/nav-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Stack } from "@speakeasy-api/moonshine";
import * as React from "react";
import { GramLogo } from "./gram-logo";
import { ProjectMenu } from "./project-menu";
import { Type } from "./ui/type";

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const routes = useRoutes();

  const topNavGroups = {
    create: [routes.openapi, routes.customTools, routes.prompts],
    curate: [routes.toolsets, routes.environments, routes.integrations],
    connect: [routes.mcp, routes.sdk, routes.slackApp],
  };

  const bottomNav = [routes.settings, routes.docs];

  const playgroundCTA = (
    <SidebarMenuButton
      tooltip={routes.playground.title}
      className={cn(
        "bg-primary! text-primary-foreground! hover:bg-primary/90 hover:text-primary-foreground min-w-8 trans",
        routes.playground.active && "border-violet-300 border-2 scale-105" // TODO rainbow
      )}
      href={routes.playground.href()}
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
              <Stack direction={"horizontal"} gap={2}>
                <routes.openapi.Link>
                  <GramLogo className="text-3xl" />
                </routes.openapi.Link>
                <Type variant="small" muted className="self-end">
                  v0.5.3 (alpha)
                </Type>
              </Stack>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem className="flex items-center gap-2">
                {playgroundCTA}
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
        {Object.entries(topNavGroups).map(([label, items]) => (
          <SidebarGroup key={label}>
            <SidebarGroupLabel>{label}</SidebarGroupLabel>
            <SidebarGroupContent className="flex flex-col gap-6">
              <NavMenu items={items} />
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
        <SidebarGroup className="mt-auto">
          <SidebarGroupContent>
            <NavMenu items={bottomNav} />
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter>
        <ProjectMenu />
      </SidebarFooter>
    </Sidebar>
  );
}
