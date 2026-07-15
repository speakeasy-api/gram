import { useMemo } from "react";
import { useTelemetry } from "@/contexts/Telemetry";
import { Scope } from "@gram/client/models/components/rolegrant.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
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
 * Honors the same feature flags the sidebar uses to gate Deployments,
 * Assistants, and demo pages.
 *
 * The returned array is memoized so consumers can safely use it as a `useEffect`
 * dependency without re-running every render (this hook feeds the command
 * palette's action-registration effect in `App.tsx`).
 */
export function useProjectNavRoutes(): ProjectNavRoute[] {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const { data: productFeatures } = useProductFeatures();

  const isAssistantsEnabled = telemetry.isFeatureEnabled("assistants") ?? false;
  // Default true: opt-out via PostHog org-group targeting on `gram-deployments-page`.
  const isDeploymentsPageEnabled =
    telemetry.isFeatureEnabled("gram-deployments-page") ?? true;
  const isSkillsEnabled = productFeatures?.skillsEnabled === true;

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
        route: routes.clis,
        scope: isSkillsEnabled ? ["skill:read"] : read,
      },
      { route: routes.plugins, scope: readWrite },
      { route: routes.environments, scope: readWrite },
      { route: routes.employees, scope: observe },
      { route: routes.costs, scope: observe },
      { route: routes.insights, scope: observe },
      { route: routes.agentSessions, scope: observe },
      { route: routes.logs, scope: observe },
      { route: routes.riskOverview, scope: read },
      { route: routes.policyCenter, scope: readWrite },
      { route: routes.riskEvents, scope: ["org:admin"] },
      { route: routes.shadowMCP, scope: readWrite },
      { route: routes.detectionRules, scope: readWrite },
      { route: routes.settings, scope: ["project:write"] },
    ];
  }, [routes, isAssistantsEnabled, isDeploymentsPageEnabled, isSkillsEnabled]);
}
