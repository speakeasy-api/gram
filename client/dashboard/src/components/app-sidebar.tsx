import { NavButton, NavMenu } from "@/components/nav-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { useSession } from "@/contexts/Auth";
import { AppRoute, useRoutes } from "@/routes";
import { useGetPeriodUsage } from "@gram/client/react-query";
import { cn, Stack } from "@speakeasy-api/moonshine";
import {
  AlertTriangleIcon,
  ChartNoAxesCombinedIcon,
  MinusIcon,
  TestTube2Icon,
} from "lucide-react";
import * as React from "react";
import { useState } from "react";
import { FeatureRequestModal } from "./FeatureRequestModal";
import { Button } from "./ui/button";
import { Type } from "./ui/type";

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const routes = useRoutes();

  const [metricsModalOpen, setMetricsModalOpen] = React.useState(false);
  const [isUpgradeModalOpen, setIsUpgradeModalOpen] = useState(false);

  const navGroups = {
    connect: [routes.sources, routes.catalog, routes.playground] as AppRoute[],
    build: [routes.elements, routes.mcp],
    observe: [routes.logs],
    settings: [routes.settings, routes.billing, routes.docs] as AppRoute[],
  };

  return (
    <Sidebar collapsible="offcanvas" {...props}>
      <SidebarContent className="pt-2">
        <SidebarGroup>
          <SidebarGroupContent>
            <NavMenu items={[routes.home]} />
          </SidebarGroupContent>
        </SidebarGroup>
        {Object.entries(navGroups).map(([label, items]) => (
          <SidebarGroup key={label}>
            <SidebarGroupLabel>{label}</SidebarGroupLabel>
            <SidebarGroupContent>
              <NavMenu items={items}>
                {label === "observe" && (
                  <SidebarMenuItem>
                    <NavButton
                      title="Metrics"
                      Icon={ChartNoAxesCombinedIcon}
                      onClick={() => setMetricsModalOpen(true)}
                    />
                  </SidebarMenuItem>
                )}
              </NavMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>
      <SidebarFooter>
        <FreeTierExceededNotification />
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
        isOpen={isUpgradeModalOpen}
        onClose={() => setIsUpgradeModalOpen(false)}
        title="Access Logs"
        description="Logs are available for Pro and Enterprise customers. Upgrade your account to access detailed logging and analytics for your tools."
        actionType="logs_page_access"
        icon={TestTube2Icon}
        accountUpgrade={true}
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
