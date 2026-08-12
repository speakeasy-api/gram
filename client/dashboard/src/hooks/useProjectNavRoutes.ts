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
   * with the sidebar when scopes change there.
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
  const assistantsFlag = useFeatureFlag(FEATURE_FLAGS.assistants);
  const deploymentsPageFlag = useFeatureFlag(FEATURE_FLAGS.deploymentsPage);
  const riskWatchdogFlag = useFeatureFlag(FEATURE_FLAGS.riskWatchdog);
  const [isOrgMemoryEnabled] = useOrgMemoryDeveloperToggle();

  // Assistants is opt-in: unavailable flags remain hidden.
  const isAssistantsEnabled = assistantsFlag.status === "enabled";
  // Deployments is opt-out: it remains visible unless PostHog explicitly
  // resolves the flag to disabled.
  const isDeploymentsPageEnabled = deploymentsPageFlag.status !== "disabled";
  // Watchdog is opt-in like Assistants: unavailable flags remain hidden.
  const isRiskWatchdogEnabled = riskWatchdogFlag.status === "enabled";

  return useMemo<ProjectNavRoute[]>(() => {
    const read: Scope[] = ["project:read"];
    const readWrite: Scope[] = ["project:read", "project:write"];
    // The Observe surface is gated on org:admin at the page level (each page
    // renders an "Access restricted" notice for non-admins, like the Secure
    // section). The nav items themselves stay visible to any project member
    // (project:read) so the group isn't silently hidden — mirrors Secure's
    // riskOverview.
    const observe: Scope[] = ["project:read"];
    return [
      { route: routes.home, scope: read },
      { route: routes.chat, scope: read },
      { route: routes.sources, scope: readWrite },
      { route: routes.catalog, scope: ["project:read", "mcp:write"] },
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
      { route: routes.employees, scope: observe },
      { route: routes.costs, scope: observe },
      { route: routes.insights, scope: observe },
      { route: routes.agentSessions, scope: observe },
      ...(isOrgMemoryEnabled
        ? [{ route: routes.orgMemory, scope: observe }]
        : []),
      { route: routes.logs, scope: observe },
      // Watchdog supersedes the Risk Overview and Risk Events pages: with the
      // flag on, it is the Secure section's landing surface and the two
      // legacy nav items hide (their routes stay reachable by direct URL).
      ...(isRiskWatchdogEnabled
        ? [{ route: routes.watchdog, scope: read }]
        : [{ route: routes.riskOverview, scope: read }]),
      { route: routes.policyCenter, scope: readWrite },
      ...(isRiskWatchdogEnabled
        ? []
        : [{ route: routes.riskEvents, scope: ["org:admin"] as Scope[] }]),
      { route: routes.shadowMCP, scope: readWrite },
      { route: routes.detectionRules, scope: readWrite },
      { route: routes.settings, scope: ["project:write"] },
    ];
  }, [
    routes,
    projectId,
    isAssistantsEnabled,
    isDeploymentsPageEnabled,
    isOrgMemoryEnabled,
    isRiskWatchdogEnabled,
  ]);
}
