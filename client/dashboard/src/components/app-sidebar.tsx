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
import { useSession } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { useGetPeriodUsage } from "@gram/client/react-query";
import { cn, Stack } from "@speakeasy-api/moonshine";
import {
  AlertTriangleIcon,
  ChartNoAxesCombinedIcon,
  MinusIcon,
  TestTubeDiagonal,
} from "lucide-react";
import * as React from "react";
import { FeatureRequestModal } from "./FeatureRequestModal";
import { GramLogo } from "./gram-logo";
import { ProjectMenu } from "./project-menu";
import { Button } from "./ui/button";
import { Type } from "./ui/type";

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const routes = useRoutes();
  const [metricsModalOpen, setMetricsModalOpen] = React.useState(false);
  const [evalsModalOpen, setEvalsModalOpen] = React.useState(false);

  const topNavGroups = {
    create: [routes.toolsets, routes.customTools, routes.prompts],
    consume: [routes.playground, routes.mcp, routes.environments],
  };

  const bottomNav = [
    routes.deployments,
    routes.billing,
    routes.settings,
    routes.docs,
  ];

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
                <Stack direction={"horizontal"} align={"center"} gap={2}>
                  <GramLogo className="w-25" />
                  <Type variant="small" muted>
                    v0.8.7 (beta)
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
                  title="Logs"
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
        <FreeTierExceededNotification />
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
        title="Logs Coming Soon"
        description="Logs are coming soon! We'll let you know when this feature is available."
        actionType="evals"
        icon={TestTubeDiagonal}
      />
    </Sidebar>
  );
}

const FreeTierExceededNotification = () => {
  const session = useSession();
  const { data: usage } = useGetPeriodUsage(undefined, undefined, {
    throwOnError: false,
  });
  const routes = useRoutes();

  if (!usage || !session || session.gramAccountType !== "free") {
    return null;
  }

  if (
    usage.toolCalls > usage.maxToolCalls ||
    usage.servers > usage.maxServers
  ) {
    return (
      <PersistentNotification variant="error">
        <Stack direction="vertical" gap={3} className="h-full">
          <Stack direction="horizontal" align="center" gap={1}>
            <AlertTriangleIcon className="w-4 h-4" />
            <Type variant="subheading">Free tier exceeded</Type>
          </Stack>
          <Type small>
            You've used{" "}
            <span className="font-medium">
              {usage.toolCalls} / {usage.maxToolCalls} tool calls
            </span>{" "}
            and{" "}
            <span className="font-medium">
              {usage.servers} / {usage.maxServers} servers
            </span>
            .
          </Type>
          <Type small>
            Your MCP server will be disabled soon. Upgrade to continue using
            Gram.
          </Type>
          <routes.billing.Link className="w-full mt-auto">
            <Button size="sm" className="w-full">
              Billing →
            </Button>
          </routes.billing.Link>
        </Stack>
      </PersistentNotification>
    );
  }

  return null;
};

const PersistentNotification = ({
  variant = "default",
  className,
  children,
}: {
  variant?: "default" | "warning" | "error";
  className?: string;
  children: React.ReactNode;
}) => {
  const [isMinimized, setIsMinimized] = React.useState(false);

  const variantClass = {
    default: "bg-card border-border",
    warning:
      "bg-warning-softest border-warning-foreground text-warning-foreground",
    error: "bg-destructive-softest border-destructive text-destructive",
  }[variant];

  const closeButton = (
    <Button
      variant="ghost"
      size="icon"
      className="absolute top-1 right-1 hover:bg-transparent"
      onClick={() => setIsMinimized(true)}
    >
      <MinusIcon className="w-4 h-4" />
    </Button>
  );

  let classes =
    "absolute bottom-2 left-1/2 h-[236px] w-[236px] -translate-x-1/2 rounded-lg p-4 border trans overflow-clip ";
  if (isMinimized) {
    classes +=
      "h-[12px] w-[12px] left-2 translate-x-0 cursor-pointer hover:scale-110";
  }

  return (
    <div
      className={cn(classes, variantClass, className)}
      onClick={() => setIsMinimized(false)}
    >
      {!isMinimized && children}
      {!isMinimized && closeButton}
      {isMinimized && (
        <Button
          variant="ghost"
          size="icon"
          className="flex items-center justify-center h-full w-full"
        >
          <Type>?</Type>
        </Button>
      )}
    </div>
  );
};
