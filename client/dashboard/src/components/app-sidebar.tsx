import { NavButton, NavMenu } from "@/components/nav-menu";
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
import { useTelemetry } from "@/contexts/Telemetry";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Stack } from "@speakeasy-api/moonshine";
import { ChartNoAxesCombinedIcon, TestTubeDiagonal } from "lucide-react";
import * as React from "react";
import { GramLogo } from "./gram-logo";
import { ProjectMenu } from "./project-menu";
import { Type } from "./ui/type";

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const routes = useRoutes();
  const telemetry = useTelemetry();

  const topNavGroups = {
    create: [
      routes.openapi,
      routes.customTools,
      routes.prompts,
      routes.environments,
    ],
    consume: [routes.playground, routes.mcp, routes.sdks],
  };

  const bottomNav = [routes.deployments, routes.settings, routes.docs];

  const primaryCTA = (
    <SidebarMenuButton
      className={cn("min-w-8 trans font-light")}
      href={routes.toolsets.href()}
      isActive={routes.toolsets.active}
    >
      <routes.toolsets.Icon />
      <span>{routes.toolsets.title}</span>
    </SidebarMenuButton>
  );

  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem className="group/logo">
            <routes.home.Link className="hover:no-underline!">
              <SidebarMenuButton
                asChild
                className="data-[slot=sidebar-menu-button]:!p-1.5 h-12"
              >
                <Stack direction={"horizontal"} gap={2}>
                  <GramLogo className="text-3xl" />
                  <Type variant="small" muted className="self-end">
                    v0.7.0 (beta)
                  </Type>
                </Stack>
              </SidebarMenuButton>
            </routes.home.Link>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        {Object.entries(topNavGroups).map(([label, items]) => (
          <SidebarGroup key={label}>
            <SidebarGroupLabel>{label}</SidebarGroupLabel>
            <SidebarGroupContent>
              {label === "create" && (
                <SidebarMenuItem className="flex items-center mb-2">
                  {primaryCTA}
                </SidebarMenuItem>
              )}
              <NavMenu items={items} />
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
        <SidebarGroup>
          <SidebarGroupLabel>Evaluate</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <NavButton
                  title="Metrics"
                  Icon={ChartNoAxesCombinedIcon}
                  onClick={() => {
                    alert("Metrics are coming soon!");
                    telemetry.capture("feature_requested", {
                      action: "metrics",
                    });
                  }}
                />
              </SidebarMenuItem>
              <SidebarMenuItem>
                <NavButton
                  title="Evals"
                  Icon={TestTubeDiagonal}
                  onClick={() => {
                    alert("Evals are coming soon!");
                    telemetry.capture("feature_requested", {
                      action: "evals",
                    });
                  }}
                />
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
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
