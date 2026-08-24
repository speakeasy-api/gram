import * as React from "react";

import { AppRoute, useOrgRoutes, useRoutes } from "@/routes";
import { MinusIcon, TestTube2Icon } from "lucide-react";
import { NavButton, NavGroupProvider } from "@/components/nav-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuItem,
  SidebarTrigger,
} from "@/components/ui/Sidebar";
import { useMemo, useState } from "react";

import { BuiltInMcpSidebarNav } from "./built-in-mcp-sidebar-nav";
import { Button } from "./ui/Button";
import { HatchRule } from "./hatch-rule";
import { FeatureRequestModal } from "./FeatureRequestModal";
import { GramLogo } from "./gram-logo";
import { Icon } from "@/components/ui/Icon";
import { InsightsDockResumeButton } from "./insights-dock-resume-button";
import { Link } from "react-router";
import { McpDetailSidebarNav } from "./mcp-detail-sidebar-nav";
import { McpServerXSidebarNav } from "./mcp-server-x-sidebar-nav";
import { OnboardingResumeButton } from "./onboarding-resume-button";
import { PlatformMcpSidebarCta } from "./platform-mcp-sidebar-cta";
import { ProjectGuideSidebarCta } from "./project-guide-sidebar-cta";
import { PluginDetailSidebarNav } from "./plugin-detail-sidebar-nav";
import type { ProjectNavRoute } from "@/hooks/useProjectNavRoutes";
import { RequireScope } from "./require-scope";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { ScopeGatedNavGroup } from "@/components/scope-gated-nav-group";
import { SidebarNavSkeleton } from "./sidebar-nav-skeleton";
import { SidebarUserMenu } from "./sidebar-user-menu";
import { SkillDetailSidebarNav } from "./skill-detail-sidebar-nav";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { TrialStatusCard } from "./trial-status-card";
import { cn } from "@/lib/utils";
import { useGetPeriodUsage } from "@gram/client/react-query/getPeriodUsage.js";
import { useNavArea } from "@/hooks/useNavArea";
import { useProductTier } from "@/hooks/useProductTier";
import { useProjectNavRoutes } from "@/hooks/useProjectNavRoutes";
import { useRBAC } from "@/hooks/useRBAC";
import { useSidebar } from "@/components/ui/Sidebar/sidebar-context";
import { useSlugs } from "@/contexts/Sdk";

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
  const [isUpgradeModalOpen, setIsUpgradeModalOpen] = useState(false);

  // useProjectNavRoutes owns feature-flag and scope decisions for both the
  // sidebar and command palette so the two surfaces cannot drift.
  const allNavRoutes = useProjectNavRoutes();
  const navAccess = useMemo(() => {
    const map = new Map<string, ProjectNavRoute>();
    for (const entry of allNavRoutes) map.set(entry.route.url, entry);
    return map;
  }, [allNavRoutes]);
  const isAssistantsEnabled = navAccess.has(routes.assistants.url);
  const isOrgMemoryEnabled = navAccess.has(routes.orgMemory.url);
  const isDeploymentsPageEnabled = navAccess.has(routes.deployments.url);
  const isRiskWatchdogEnabled = navAccess.has(routes.watchdog.url);

  // Shared with the page-title eyebrow (Page.Eyebrow) so the sidebar group
  // highlight and the page header always agree on the area. "Organization"
  // labels org-level pages in the header but is not a sidebar group.
  const navArea = useNavArea();
  const activeGroup = navArea === "Organization" ? undefined : navArea;

  // Find the specific active route title for the sliding highlight. Shared with
  // the command palette via useProjectNavRoutes so the two stay in sync.
  const activeRoute = allNavRoutes.find((entry) => entry.route.active)?.route;
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

  const isWideSidebarDetailRoute =
    routes.mcp.details.active ||
    routes.mcp.x.active ||
    routes.mcp.builtIn.active ||
    routes.skills.detail.active ||
    routes.plugins.detail.active;

  let sidebarContent: React.ReactNode;
  if (rbacLoading) {
    // Shaped like the real list below — 3 top-level items, the divider, the
    // 4 collapsed groups, then Settings — at the same spacing, so resolving
    // the grants swaps the rows out without shifting the nav.
    sidebarContent = (
      <SidebarNavSkeleton
        rows={8}
        divideAfter={3}
        className="gap-0.5 px-2 group-data-[collapsible=icon]:px-0"
      />
    );
  } else if (routes.mcp.details.active) {
    sidebarContent = <McpDetailSidebarNav />;
  } else if (routes.mcp.x.active) {
    sidebarContent = <McpServerXSidebarNav />;
  } else if (routes.mcp.builtIn.active) {
    sidebarContent = <BuiltInMcpSidebarNav />;
  } else if (routes.skills.detail.active) {
    sidebarContent = <SkillDetailSidebarNav />;
  } else if (routes.plugins.detail.active) {
    sidebarContent = <PluginDetailSidebarNav />;
  } else {
    sidebarContent = (
      <NavGroupProvider activeGroup={activeGroup} activeItem={activeItem}>
        <SidebarMenu className="gap-0.5 px-2 group-data-[collapsible=icon]:px-0">
          {/* Home — the org-scoped app (was the "Organization settings"
              footer action); the project's own landing page sits below it as
              "Project Overview". Scoped to match OrgSidebar's own Home item,
              so it only shows to users who can open the page it links to. */}
          <RequireScope
            scope={["org:read", "project:read", "org:admin"]}
            level="section"
          >
            <SidebarMenuItem>
              <NavButton
                title="Home"
                href={`/${orgSlug}`}
                Icon={(p) => <Icon {...p} name="building" />}
              />
            </SidebarMenuItem>
          </RequireScope>

          {/* Project overview — top-level, no group */}
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
          <li aria-hidden="true" className="my-2 px-1">
            <div className="border-border border-t" />
          </li>

          {/* Observe group */}
          <ScopeGatedNavGroup
            label="Observe"
            Icon={(p) => <Icon {...p} name="eye" />}
            items={[
              { item: routes.costs, ...accessFor(routes.costs) },
              { item: routes.insights, ...accessFor(routes.insights) },
              {
                item: routes.agentSessions,
                ...accessFor(routes.agentSessions),
              },
              ...(isOrgMemoryEnabled
                ? [{ item: routes.orgMemory, ...accessFor(routes.orgMemory) }]
                : []),
              { item: routes.logs, ...accessFor(routes.logs) },
              { item: routes.employees, ...accessFor(routes.employees) },
            ]}
          />

          {/* Secure group */}
          <ScopeGatedNavGroup
            label="Secure"
            Icon={(p) => <Icon {...p} name="shield" />}
            items={[
              // Watchdog supersedes Risk Overview: exactly one of the two
              // shows, mirroring useProjectNavRoutes. Risk Events sits below
              // the landing surface in both modes.
              ...(isRiskWatchdogEnabled
                ? [{ item: routes.watchdog, ...accessFor(routes.watchdog) }]
                : [
                    {
                      item: routes.riskOverview,
                      ...accessFor(routes.riskOverview),
                    },
                  ]),
              { item: routes.riskEvents, ...accessFor(routes.riskEvents) },
              { item: routes.policyCenter, ...accessFor(routes.policyCenter) },
              { item: routes.shadowMCP, ...accessFor(routes.shadowMCP) },
            ]}
          />

          {/* Connect group */}
          <ScopeGatedNavGroup
            label="Connect"
            Icon={(p) => <Icon {...p} name="plug" />}
            items={[
              { item: routes.sources, ...accessFor(routes.sources) },
              { item: routes.catalog, ...accessFor(routes.catalog) },
              { item: routes.playground, ...accessFor(routes.playground) },
              ...(isDeploymentsPageEnabled
                ? [
                    {
                      item: routes.deployments,
                      ...accessFor(routes.deployments),
                    },
                  ]
                : []),
            ]}
          />

          {/* Distribute group */}
          <ScopeGatedNavGroup
            label="Distribute"
            Icon={(p) => <Icon {...p} name="hammer" />}
            items={[
              { item: routes.mcp, ...accessFor(routes.mcp) },
              ...(isAssistantsEnabled
                ? [{ item: routes.assistants, ...accessFor(routes.assistants) }]
                : []),
              { item: routes.skills, ...accessFor(routes.skills) },
              { item: routes.plugins, ...accessFor(routes.plugins) },
              { item: routes.environments, ...accessFor(routes.environments) },
            ]}
          />

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
        isWideSidebarDetailRoute
          ? ({ "--sidebar-width": "22rem" } as React.CSSProperties)
          : undefined
      }
      {...props}
    >
      {/* Logo row only — the project switcher now lives in the page header.
          The row is exactly --header-height and closes with the same crosshatch
          rule the page header uses, so the divider reads as one line running
          across both panes. */}
      <SidebarHeader className="gap-0 p-0">
        <div className="flex h-(--header-height) items-center justify-between gap-2 px-2 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0">
          <Link
            to={`/${orgSlug}`}
            className="flex h-full items-center px-1 hover:no-underline group-data-[collapsible=icon]:hidden"
          >
            <GramLogo className="w-28" />
          </Link>
          {/* Collapse control sits beside the logo (WorkOS placement); search
              moved out to the page header. */}
          <SidebarTrigger />
        </div>
        <HatchRule />
      </SidebarHeader>
      <SidebarContent className="pt-2">{sidebarContent}</SidebarContent>
      <SidebarFooter className="border-t">
        <FreeTierExceededNotification />
        <div className="mb-2 flex flex-col gap-1.5">
          <TrialStatusCard />
          <OnboardingResumeButton />
          <ProjectGuideSidebarCta />
          <PlatformMcpSidebarCta />
          <InsightsDockResumeButton />
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
          <Text variant="subheading">Limits exceeded</Text>
          <Text small>
            Free tier limits exceeded. Upgrade to continue using the platform.
          </Text>
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
      variant="tertiary"
      size="md"
      className="absolute top-0 right-0 hover:bg-transparent"
      onClick={() => setIsMinimized(true)}
    >
      <MinusIcon className="h-4 w-4" />
    </Button>
  );

  let classes =
    "absolute bottom-2 left-1/2 h-[180px] w-[180px] -translate-x-1/2 p-4 border trans overflow-clip ";
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
          variant="tertiary"
          size="md"
          className="flex h-full w-full items-center justify-center"
        >
          <Text>?</Text>
        </Button>
      )}
    </div>
  );
};
