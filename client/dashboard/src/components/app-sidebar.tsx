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
  SidebarHeader,
  SidebarMenu,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { GramLogo } from "./gram-logo";
import { CommandPaletteTrigger } from "./command-palette/CommandPaletteTrigger";
import { WorkspaceSwitcher } from "./workspace-switcher";
import { InsightsDockResumeButton } from "./insights-dock-resume-button";
import { BuiltInMcpSidebarNav } from "./built-in-mcp-sidebar-nav";
import { McpDetailSidebarNav } from "./mcp-detail-sidebar-nav";
import { McpServerXSidebarNav } from "./mcp-server-x-sidebar-nav";
import { OnboardingResumeButton } from "./onboarding-resume-button";
import { SidebarFooterAction } from "./sidebar-footer-action";
import { SidebarUserMenu } from "./sidebar-user-menu";
import { useSidebar } from "@/components/ui/sidebar-context";
import { useSlugs } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRBAC } from "@/hooks/useRBAC";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { SidebarNavSkeleton } from "./sidebar-nav-skeleton";
import { useProductTier } from "@/hooks/useProductTier";
import { useProjectNavRoutes } from "@/hooks/useProjectNavRoutes";
import type { ProjectNavRoute } from "@/hooks/useProjectNavRoutes";
import { AppRoute, useOrgRoutes, useRoutes } from "@/routes";
import { useGetPeriodUsage } from "@gram/client/react-query/getPeriodUsage.js";
import { cn, Icon, Stack } from "@speakeasy-api/moonshine";
import { MinusIcon, Settings, TestTube2Icon } from "lucide-react";
import * as React from "react";
import { useMemo, useState } from "react";
import { Link } from "react-router";
import { RequireScope } from "./require-scope";
import { FeatureRequestModal } from "./FeatureRequestModal";
import { Button } from "./ui/button";
import { Type } from "./ui/type";

function ScopeGatedNavItem({
  item,
  scope,
  resourceId,
}: {
  item: AppRoute;
  scope: Scope | Scope[];
  resourceId?: string;
}) {
  return (
    <RequireScope scope={scope} resourceId={resourceId} level="section">
      <CollapsibleNavItem item={item} />
    </RequireScope>
  );
}

function ScopeGatedTopLevelItem({
  item,
  scope,
  resourceId,
}: {
  item: AppRoute;
  scope: Scope | Scope[];
  resourceId?: string;
}) {
  return (
    <RequireScope scope={scope} resourceId={resourceId} level="section">
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

export function AppSidebar({
  ...props
}: React.ComponentProps<typeof Sidebar>): React.JSX.Element {
  const routes = useRoutes();
  const { orgSlug } = useSlugs();
  const { state } = useSidebar();
  // While grants reload (e.g. right after switching projects, when the query
  // cache is cleared), show a skeleton so the scope-gated nav doesn't flash empty.
  const { isLoading: rbacLoading } = useRBAC();
  const telemetry = useTelemetry();
  const [isUpgradeModalOpen, setIsUpgradeModalOpen] = useState(false);

  const isAssistantsEnabled = telemetry.isFeatureEnabled("assistants") ?? false;
  // Default true: opt-out via PostHog org-group targeting on `gram-deployments-page`.
  const isDeploymentsPageEnabled =
    telemetry.isFeatureEnabled("gram-deployments-page") ?? true;
  // Prototype: opt-in via PostHog `gram-budgets-page`.
  const isBudgetsEnabled =
    telemetry.isFeatureEnabled("gram-budgets-page") ?? false;

  const connectActive = [
    routes.sources,
    routes.catalog,
    routes.playground,
    ...(isDeploymentsPageEnabled ? [routes.deployments] : []),
  ].some((r) => r.active);

  const distributeActive = [
    routes.mcp,
    routes.skills,
    routes.plugins,
    routes.environments,
    ...(isAssistantsEnabled ? [routes.assistants] : []),
  ].some((r) => r.active);

  const observeActive = [
    routes.employees,
    routes.costs,
    routes.budgets,
    routes.insights,
    routes.agentSessions,
    routes.logs,
  ].some((r) => r.active);

  const securityActive = [
    routes.riskOverview,
    routes.policyCenter,
    routes.riskEvents,
    routes.shadowMCP,
    routes.approvalRequests,
    routes.detectionRules,
  ].some((r) => r.active);

  let activeGroup: string | undefined;
  if (observeActive) {
    activeGroup = "Observe";
  } else if (securityActive) {
    activeGroup = "Secure";
  } else if (connectActive) {
    activeGroup = "Connect";
  } else if (distributeActive) {
    activeGroup = "Distribute";
  }

  // Find the specific active route title for the sliding highlight. Shared with
  // the command palette via useProjectNavRoutes so the two stay in sync.
  const allNavRoutes = useProjectNavRoutes();
  const activeRoute = allNavRoutes.find((entry) => entry.route.active)?.route;
  // Single source of truth for per-page scopes, shared with the command palette
  // via useProjectNavRoutes so nav visibility and palette visibility can't drift.
  const navAccess = useMemo(() => {
    const map = new Map<string, ProjectNavRoute>();
    for (const entry of allNavRoutes) map.set(entry.route.url, entry);
    return map;
  }, [allNavRoutes]);
  const accessFor = (
    route: AppRoute,
  ): Pick<ProjectNavRoute, "scope" | "resourceId"> => {
    const entry = navAccess.get(route.url);
    return entry
      ? { scope: entry.scope, resourceId: entry.resourceId }
      : { scope: ["project:read"] };
  };
  // In collapsed mode, sub-items are hidden — fall back to group highlight.
  // Top-level items (Home, Settings) have no activeGroup, so keep activeItem for those.
  const activeItem =
    state === "collapsed" && activeGroup ? undefined : activeRoute?.title;

  const isMcpDetailRoute =
    routes.mcp.details.active ||
    routes.mcp.x.active ||
    routes.mcp.builtIn.active;

  let sidebarContent: React.ReactNode;
  if (rbacLoading) {
    sidebarContent = <SidebarNavSkeleton />;
  } else if (routes.mcp.details.active) {
    sidebarContent = <McpDetailSidebarNav />;
  } else if (routes.mcp.x.active) {
    sidebarContent = <McpServerXSidebarNav />;
  } else if (routes.mcp.builtIn.active) {
    sidebarContent = <BuiltInMcpSidebarNav />;
  } else {
    sidebarContent = (
      <NavGroupProvider
        activeGroup={activeGroup}
        defaultOpenGroups={
          !activeGroup
            ? ["Observe", "Secure", "Connect", "Distribute"]
            : undefined
        }
        activeItem={activeItem}
      >
        <SidebarMenu className="gap-1 px-2 group-data-[collapsible=icon]:px-0">
          {/* Home — top-level, no group */}
          <ScopeGatedTopLevelItem
            item={routes.home}
            {...accessFor(routes.home)}
          />

          {/* Chat — top-level, no group; a full-page entry to the
                  Project Assistant alongside the docked composer */}
          <ScopeGatedTopLevelItem
            item={routes.chat}
            {...accessFor(routes.chat)}
          />

          {/* Divider: sets Home + Chat apart from the grouped nav below */}
          <li aria-hidden="true" className="my-3 px-1">
            <div className="border-border border-t" />
          </li>

          {/* Observe group */}
          <CollapsibleNavGroup
            label="Observe"
            Icon={(p) => <Icon {...p} name="eye" />}
            defaultHref={routes.costs.href()}
          >
            <ScopeGatedNavItem
              item={routes.costs}
              {...accessFor(routes.costs)}
            />
            {isBudgetsEnabled && (
              <ScopeGatedNavItem
                item={routes.budgets}
                {...accessFor(routes.budgets)}
              />
            )}
            <ScopeGatedNavItem
              item={routes.insights}
              {...accessFor(routes.insights)}
            />
            <ScopeGatedNavItem
              item={routes.agentSessions}
              {...accessFor(routes.agentSessions)}
            />
            <ScopeGatedNavItem item={routes.logs} {...accessFor(routes.logs)} />
            <ScopeGatedNavItem
              item={routes.employees}
              {...accessFor(routes.employees)}
            />
          </CollapsibleNavGroup>

          {/* Secure group */}
          <CollapsibleNavGroup
            label="Secure"
            Icon={(p) => <Icon {...p} name="shield" />}
            defaultHref={routes.riskOverview.href()}
            stage="beta"
          >
            <ScopeGatedNavItem
              item={routes.riskOverview}
              {...accessFor(routes.riskOverview)}
            />
            <ScopeGatedNavItem
              item={routes.policyCenter}
              {...accessFor(routes.policyCenter)}
            />
            <ScopeGatedNavItem
              item={routes.riskEvents}
              {...accessFor(routes.riskEvents)}
            />
            <ScopeGatedNavItem
              item={routes.shadowMCP}
              {...accessFor(routes.shadowMCP)}
            />
            <ScopeGatedNavItem
              item={routes.detectionRules}
              {...accessFor(routes.detectionRules)}
            />
          </CollapsibleNavGroup>

          {/* Connect group */}
          <CollapsibleNavGroup
            label="Connect"
            Icon={(p) => <Icon {...p} name="plug" />}
            defaultHref={routes.sources.href()}
          >
            <ScopeGatedNavItem
              item={routes.sources}
              {...accessFor(routes.sources)}
            />
            <ScopeGatedNavItem
              item={routes.catalog}
              {...accessFor(routes.catalog)}
            />
            <ScopeGatedNavItem
              item={routes.playground}
              {...accessFor(routes.playground)}
            />
            {isDeploymentsPageEnabled && (
              <ScopeGatedNavItem
                item={routes.deployments}
                {...accessFor(routes.deployments)}
              />
            )}
          </CollapsibleNavGroup>

          {/* Distribute group */}
          <CollapsibleNavGroup
            label="Distribute"
            Icon={(p) => <Icon {...p} name="hammer" />}
            defaultHref={routes.mcp.href()}
          >
            <ScopeGatedNavItem item={routes.mcp} {...accessFor(routes.mcp)} />
            {isAssistantsEnabled && (
              <ScopeGatedNavItem
                item={routes.assistants}
                {...accessFor(routes.assistants)}
              />
            )}
            <ScopeGatedNavItem
              item={routes.skills}
              {...accessFor(routes.skills)}
            />
            <ScopeGatedNavItem
              item={routes.plugins}
              {...accessFor(routes.plugins)}
            />
            <ScopeGatedNavItem
              item={routes.environments}
              {...accessFor(routes.environments)}
            />
          </CollapsibleNavGroup>

          {/* Settings — top-level, no group */}
          <ScopeGatedTopLevelItem
            item={routes.settings}
            {...accessFor(routes.settings)}
          />
        </SidebarMenu>
      </NavGroupProvider>
    );
  }

  return (
    <Sidebar
      collapsible="icon"
      style={
        isMcpDetailRoute
          ? ({ "--sidebar-width": "22rem" } as React.CSSProperties)
          : undefined
      }
      {...props}
    >
      <SidebarHeader className="gap-3 pb-3">
        <div className="flex items-center justify-between gap-2 group-data-[collapsible=icon]:justify-center">
          <Link
            to={`/${orgSlug}`}
            className="flex h-(--header-height) items-center px-1 hover:no-underline group-data-[collapsible=icon]:hidden"
          >
            <GramLogo className="w-28" />
          </Link>
          <CommandPaletteTrigger />
        </div>
        <WorkspaceSwitcher />
      </SidebarHeader>
      <SidebarContent className="pt-2">{sidebarContent}</SidebarContent>
      <SidebarFooter className="border-t">
        <FreeTierExceededNotification />
        <div className="mb-2 flex flex-col gap-1.5">
          <OnboardingResumeButton />
          <InsightsDockResumeButton />
          <SidebarFooterAction
            to={`/${orgSlug}`}
            icon={Settings}
            label="Organization settings"
          />
        </div>
        <SidebarUserMenu />
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
            Free tier limits exceeded. Upgrade to continue using the platform.
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
