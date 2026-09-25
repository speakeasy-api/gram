import { useMemo } from "react";
import { useProject } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { AppRoute, useRoutes } from "@/routes";
import { useOrgMemoryDeveloperToggle } from "./useOrgMemoryDeveloperToggle";

/** A project nav page plus the scopes that grant access to it. */
export interface ProjectNavRoute {
  route: AppRoute;
  /**
   * Scopes that grant access — the user needs ANY one of them. Mirrors the
   * per-item `scope` props on `app-sidebar.tsx`'s `ScopeGatedNavItem`s so the
   * command palette gates the same pages the sidebar does. Keep these in sync
   * with the sidebar when scopes change there. An empty array means the page
   * uses server-side ownership authorization and needs no navigation scope.
   */
  scope: Scope[];
  /** Resource selected for this route's scope check, when applicable. */
  resourceId?: string;
}

/**
 * The ordered set of project pages shown in the left sidebar nav.
 *
 * Single source of truth shared by the sidebar (`AppSidebar`) and the command
 * palette so the two never drift — the palette only lists pages a user can
 * actually reach from the nav, in the same order, behind the same scopes.
 * Honors the same feature flags the sidebar uses to gate Deployments,
 * Assistants, and demo pages.
 *
 * The returned array is memoized so consumers can safely use it as a `useEffect`
 * dependency without re-running every render (this hook feeds the command
 * palette's action-registration effect in `App.tsx`).
 */
export function useProjectNavRoutes(): ProjectNavRoute[] {
  const routes = useRoutes();
  const { id: projectId } = useProject();
  const fleetFlag = useFeatureFlag(FEATURE_FLAGS.fleet);
  const agentManagementFlag = useFeatureFlag(FEATURE_FLAGS.agentManagement);
  const userSessionsFlag = useFeatureFlag(FEATURE_FLAGS.userSessionsDashboard);
  const assistantsFlag = useFeatureFlag(FEATURE_FLAGS.assistants);
  const deploymentsPageFlag = useFeatureFlag(FEATURE_FLAGS.deploymentsPage);
  const riskWatchdogFlag = useFeatureFlag(FEATURE_FLAGS.riskWatchdog);
  const exploreFlag = useFeatureFlag(FEATURE_FLAGS.explore);
  const [isOrgMemoryEnabled] = useOrgMemoryDeveloperToggle();

  // Assistants is opt-in: unavailable flags remain hidden.
  const isAssistantsEnabled = assistantsFlag.status === "enabled";
  // Deployments is opt-out: it remains visible unless PostHog explicitly
  // resolves the flag to disabled.
  const isDeploymentsPageEnabled = deploymentsPageFlag.status !== "disabled";
  // Watchdog is opt-in like Assistants: unavailable flags remain hidden.
  const isRiskWatchdogEnabled = riskWatchdogFlag.status === "enabled";
  // Explore is opt-in while it is dogfooded: the flag is released to the
  // organizations trying it, and everyone else never sees the page.
  const isExploreEnabled = exploreFlag.status === "enabled";

  return useMemo<ProjectNavRoute[]>(() => {
    const read: Scope[] = ["project:read"];
    const readWrite: Scope[] = ["project:read", "project:write"];
    // Observe navigation stays visible to project readers. The Identities
    // roster uses that scope; identity detail resolution separately requires
    // org:read, and the remaining Observe pages render an org-admin notice.
    const observe: Scope[] = ["project:read"];
    return [
      { route: routes.home, scope: read },
      { route: routes.chat, scope: read },
      { route: routes.identities, scope: observe },
      ...(agentManagementFlag.status === "enabled"
        ? [{ route: routes.agents, scope: [] }]
        : []),
      ...(userSessionsFlag.status === "enabled"
        ? [
            {
              route: routes.mcpSessions,
              scope: read,
              resourceId: projectId,
            },
          ]
        : []),
      {
        route: routes.remoteIdentityProviders,
        scope: ["org:read", "org:admin"],
      },
      {
        route: routes.playground,
        scope: ["mcp:read", "mcp:write", "mcp:connect"],
      },
      ...(isDeploymentsPageEnabled
        ? [{ route: routes.deployments, scope: readWrite }]
        : []),
      { route: routes.mcp, scope: ["mcp:read", "mcp:write"] },
      ...(isAssistantsEnabled
        ? [{ route: routes.assistants, scope: read }]
        : []),
      {
        route: routes.skills,
        scope: ["skill:read"],
        resourceId: projectId,
      },
      { route: routes.plugins, scope: readWrite },
      { route: routes.environments, scope: readWrite },
      // Watchdog supersedes the Risk Overview page: with the flag on, it is
      // the Secure section's landing surface and the legacy overview nav item
      // hides (its route stays reachable by direct URL). Risk Events shows in
      // both modes, directly below the landing surface.
      ...(isRiskWatchdogEnabled
        ? [{ route: routes.watchdog, scope: read }]
        : [{ route: routes.riskOverview, scope: read }]),
      { route: routes.riskEvents, scope: ["org:admin"] as Scope[] },
      { route: routes.policyCenter, scope: readWrite },
      { route: routes.shadowAI, scope: readWrite },
      { route: routes.costs, scope: observe },
      ...(isExploreEnabled ? [{ route: routes.explore, scope: observe }] : []),
      { route: routes.insights, scope: observe },
      ...(fleetFlag.status === "enabled"
        ? [{ route: routes.fleet, scope: read }]
        : []),
      { route: routes.agentSessions, scope: observe },
      ...(isOrgMemoryEnabled
        ? [{ route: routes.orgMemory, scope: observe }]
        : []),
      { route: routes.logs, scope: observe },
      { route: routes.settings, scope: ["project:write"] },
    ];
  }, [
    routes,
    agentManagementFlag.status,
    fleetFlag.status,
    userSessionsFlag.status,
    projectId,
    isAssistantsEnabled,
    isDeploymentsPageEnabled,
    isExploreEnabled,
    isOrgMemoryEnabled,
    isRiskWatchdogEnabled,
  ]);
}
