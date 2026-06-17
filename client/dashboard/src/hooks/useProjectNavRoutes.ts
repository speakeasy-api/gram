import { useMemo } from "react";
import { useTelemetry } from "@/contexts/Telemetry";
import { Scope } from "@/hooks/useRBAC";
import { AppRoute, useRoutes } from "@/routes";

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
}

/**
 * The ordered set of project pages shown in the left sidebar nav.
 *
 * Single source of truth shared by the sidebar (`AppSidebar`) and the command
 * palette so the two never drift — the palette only lists pages a user can
 * actually reach from the nav, in the same order, behind the same scopes.
 * Honors the same feature flags the sidebar uses to gate Deployments and
 * Assistants.
 *
 * The returned array is memoized so consumers can safely use it as a `useEffect`
 * dependency without re-running every render (this hook feeds the command
 * palette's action-registration effect in `App.tsx`).
 */
export function useProjectNavRoutes(): ProjectNavRoute[] {
  const routes = useRoutes();
  const telemetry = useTelemetry();

  const isAssistantsEnabled = telemetry.isFeatureEnabled("assistants") ?? false;
  // Default true: opt-out via PostHog org-group targeting on `gram-deployments-page`.
  const isDeploymentsPageEnabled =
    telemetry.isFeatureEnabled("gram-deployments-page") ?? true;

  return useMemo<ProjectNavRoute[]>(() => {
    const read: Scope[] = ["project:read"];
    const readWrite: Scope[] = ["project:read", "project:write"];
    return [
      { route: routes.home, scope: read },
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
      { route: routes.clis, scope: read },
      { route: routes.plugins, scope: readWrite },
      { route: routes.environments, scope: readWrite },
      { route: routes.employees, scope: read },
      { route: routes.costs, scope: read },
      { route: routes.insights, scope: read },
      { route: routes.agentSessions, scope: read },
      { route: routes.logs, scope: read },
      { route: routes.riskOverview, scope: read },
      { route: routes.policyCenter, scope: readWrite },
      { route: routes.riskEvents, scope: ["org:admin"] },
      { route: routes.approvalRequests, scope: readWrite },
      { route: routes.detectionRules, scope: readWrite },
      { route: routes.settings, scope: ["project:write"] },
    ];
  }, [routes, isAssistantsEnabled, isDeploymentsPageEnabled]);
}
