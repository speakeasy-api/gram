import {
  CollapsibleNavGroup,
  CollapsibleNavItem,
  NavButton,
  NavGroupProvider,
} from "@/components/nav-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarMenu,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { useSlugs } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { Scope } from "@/hooks/useRBAC";
import { useProductTier } from "@/hooks/useProductTier";
import { AppRoute, useOrgRoutes, useRoutes } from "@/routes";
import { useGetPeriodUsage } from "@gram/client/react-query";
import { cn, Icon, Stack } from "@speakeasy-api/moonshine";
import { MinusIcon, TestTube2Icon, Undo2 } from "lucide-react";
import * as React from "react";
import { useState } from "react";
import { Link } from "react-router";
import { RequireScope } from "./require-scope";
import { FeatureRequestModal } from "./FeatureRequestModal";
import { Button } from "./ui/button";
import { Type } from "./ui/type";

function ScopeGatedNavItem({
  item,
  scope,
}: {
  item: AppRoute;
  scope: Scope | Scope[];
}) {
  return (
    <RequireScope scope={scope} level="section">
      <CollapsibleNavItem item={item} />
    </RequireScope>
  );
}

function ScopeGatedTopLevelItem({
  item,
  scope,
}: {
  item: AppRoute;
  scope: Scope | Scope[];
}) {
  return (
    <RequireScope scope={scope} level="section">
      <SidebarMenuItem>
        <NavButton
          title={item.title}
          href={item.href()}
          active={item.active}
          Icon={item.Icon}
          stage={item.stage}
        />
      </SidebarMenuItem>
    </RequireScope>
  );
}

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const routes = useRoutes();
  const { orgSlug } = useSlugs();
  const telemetry = useTelemetry();
  const [isUpgradeModalOpen, setIsUpgradeModalOpen] = useState(false);

  const isAssistantsEnabled = telemetry.isFeatureEnabled("assistants") ?? false;

  const connectActive = [
    routes.sources,
    routes.catalog,
    routes.playground,
    routes.deployments,
  ].some((r) => r.active);

  const buildActive = [
    routes.mcp,
    routes.clis,
    routes.plugins,
    routes.environments,
    ...(isAssistantsEnabled ? [routes.assistants] : []),
  ].some((r) => r.active);

  const observeActive = [routes.insights, routes.logs].some((r) => r.active);

  const securityActive = [routes.riskOverview, routes.policyCenter].some(
    (r) => r.active,
  );

  const activeGroup = connectActive
    ? "Connect"
    : buildActive
      ? "Build"
      : observeActive
        ? "Observe"
        : securityActive
          ? "Security"
          : undefined;

  // Find the specific active route title for the sliding highlight
  const allNavRoutes = [
    routes.home,
    routes.sources,
    routes.catalog,
    routes.playground,
    routes.deployments,
    routes.mcp,
    ...(isAssistantsEnabled ? [routes.assistants] : []),
    routes.clis,
    routes.plugins,
    routes.environments,
    routes.insights,
    routes.logs,
    routes.riskOverview,
    routes.policyCenter,
    routes.settings,
  ];
  const activeRoute = allNavRoutes.find((r) => r.active);
  const activeItem = activeRoute?.title;

  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarContent className="pt-5">
        <NavGroupProvider activeGroup={activeGroup} activeItem={activeItem}>
          <SidebarMenu className="gap-1 px-2">
            {/* Home — top-level, no group */}
            <ScopeGatedTopLevelItem item={routes.home} scope="project:read" />

            {/* Connect group */}
            <CollapsibleNavGroup
              label="Connect"
              Icon={(p) => <Icon {...p} name="plug" />}
              defaultHref={routes.sources.href()}
              isActive={connectActive}
            >
              <ScopeGatedNavItem
                item={routes.sources}
                scope={["project:read", "project:write"]}
              />
              <ScopeGatedNavItem
                item={routes.catalog}
                scope={["project:read", "mcp:write"]}
              />
              <ScopeGatedNavItem
                item={routes.playground}
                scope={["mcp:read", "mcp:write", "mcp:connect"]}
              />
              <ScopeGatedNavItem
                item={routes.deployments}
                scope={["project:read", "project:write"]}
              />
            </CollapsibleNavGroup>

            {/* Build group */}
            <CollapsibleNavGroup
              label="Build"
              Icon={(p) => <Icon {...p} name="hammer" />}
              defaultHref={routes.mcp.href()}
              isActive={buildActive}
            >
              <ScopeGatedNavItem
                item={routes.mcp}
                scope={["mcp:read", "mcp:write"]}
              />
              {isAssistantsEnabled && (
                <ScopeGatedNavItem
                  item={routes.assistants}
                  scope="project:read"
                />
              )}
              <ScopeGatedNavItem item={routes.clis} scope="project:read" />
              <ScopeGatedNavItem
                item={routes.plugins}
                scope={["project:read", "project:write"]}
              />
              <ScopeGatedNavItem
                item={routes.environments}
                scope={["project:read", "project:write"]}
              />
            </CollapsibleNavGroup>

            {/* Observe group */}
            <CollapsibleNavGroup
              label="Observe"
              Icon={(p) => <Icon {...p} name="eye" />}
              defaultHref={routes.insights.href()}
              isActive={observeActive}
            >
              <ScopeGatedNavItem item={routes.insights} scope="project:read" />
              <ScopeGatedNavItem item={routes.logs} scope="project:read" />
            </CollapsibleNavGroup>

            {/* Security group */}
            <CollapsibleNavGroup
              label="Security"
              Icon={(p) => <Icon {...p} name="shield" />}
              defaultHref={routes.riskOverview.href()}
              isActive={securityActive}
            >
              <ScopeGatedNavItem
                item={routes.riskOverview}
                scope="project:read"
              />
              <ScopeGatedNavItem
                item={routes.policyCenter}
                scope={["project:read", "project:write"]}
              />
            </CollapsibleNavGroup>

            {/* Settings — top-level, no group */}
            <ScopeGatedTopLevelItem
              item={routes.settings}
              scope="project:write"
            />
          </SidebarMenu>
        </NavGroupProvider>

        <div className="mt-auto px-2 py-3 group-data-[collapsible=icon]:px-0">
          <Link
            to={`/${orgSlug}`}
            title="Back to org"
            className="text-muted-foreground hover:text-foreground flex items-center gap-1.5 px-2 py-1 text-sm transition-colors group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0 hover:no-underline"
          >
            <Undo2 className="h-3.5 w-3.5" />
            <span className="group-data-[collapsible=icon]:hidden">
              Back to org
            </span>
          </Link>
        </div>
      </SidebarContent>
      <SidebarFooter className="group-data-[collapsible=icon]:hidden">
        <FreeTierExceededNotification />
      </SidebarFooter>
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
  const productTier = useProductTier();
  // Only fetch usage data for free-tier users — this notification is
  // irrelevant for paid/enterprise tiers and the request takes ~3s.
  const { data: usage } = useGetPeriodUsage(undefined, undefined, {
    throwOnError: false,
    enabled: productTier === "base",
  });
  const orgRoutes = useOrgRoutes();

  if (!usage || productTier !== "base") {
    return null;
  }

  if (
    usage.toolCalls > usage.includedToolCalls ||
    usage.servers > usage.includedServers
  ) {
    return (
      <PersistentNotification variant="error">
        <Stack direction="vertical" gap={3} className="h-full">
          <Type variant="subheading">Limits exceeded</Type>
          <Type small>
            Free tier limits exceeded. Upgrade to continue using Gram.
          </Type>
          <orgRoutes.billing.Link className="mt-auto w-full">
            <Button size="sm" className="w-full">
              Billing →
            </Button>
          </orgRoutes.billing.Link>
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
      className="absolute top-0 right-0 hover:bg-transparent"
      onClick={() => setIsMinimized(true)}
    >
      <MinusIcon className="h-4 w-4" />
    </Button>
  );

  let classes =
    "absolute bottom-2 left-1/2 h-[180px] w-[180px] -translate-x-1/2 rounded-lg p-4 border trans overflow-clip ";
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
          className="flex h-full w-full items-center justify-center"
        >
          <Type>?</Type>
        </Button>
      )}
    </div>
  );
};
