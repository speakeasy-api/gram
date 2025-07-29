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
import { useRoutes } from "@/routes";
import { Stack } from "@speakeasy-api/moonshine";
import { ChartNoAxesCombinedIcon, TestTubeDiagonal } from "lucide-react";
import * as React from "react";
import { FeatureRequestModal } from "./FeatureRequestModal";
import { GramLogo } from "./gram-logo";
import { ProjectMenu } from "./project-menu";
import { Type } from "./ui/type";

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const routes = useRoutes();
  const [metricsModalOpen, setMetricsModalOpen] = React.useState(false);
  const [evalsModalOpen, setEvalsModalOpen] = React.useState(false);

  const topNavGroups = {
    create: [
      routes.toolsets,
      routes.customTools,
      routes.prompts,
      routes.environments,
    ],
    consume: [routes.playground, routes.mcp, routes.sdks],
  };

  const bottomNav = [routes.deployments, routes.settings, routes.docs];

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
                    v0.8.2 (beta)
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
                  onClick={() => setMetricsModalOpen(true)}
                />
              </SidebarMenuItem>
              <SidebarMenuItem>
                <NavButton
                  title="Evals"
                  Icon={TestTubeDiagonal}
                  onClick={() => setEvalsModalOpen(true)}
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
      <FeatureRequestModal
        isOpen={metricsModalOpen}
        onClose={() => setMetricsModalOpen(false)}
        title="Metrics Coming Soon"
        description="Metrics are coming soon! We'll let you know when this feature is available."
        actionType="metrics"
        icon={ChartNoAxesCombinedIcon}
      />
      <FeatureRequestModal
        isOpen={evalsModalOpen}
        onClose={() => setEvalsModalOpen(false)}
        title="Evals Coming Soon"
        description="Evals are coming soon! We'll let you know when this feature is available."
        actionType="evals"
        icon={TestTubeDiagonal}
      />
    </Sidebar>
  );
}
