import { useTelemetry } from "@/contexts/Telemetry";
import { AppRoute, useRoutes } from "@/routes";

/**
 * The ordered set of project pages shown in the left sidebar nav.
 *
 * Single source of truth shared by the sidebar (`AppSidebar`) and the command
 * palette so the two never drift — the palette only lists pages a user can
 * actually reach from the nav, in the same order. Honors the same feature flags
 * the sidebar uses to gate Deployments and Assistants.
 */
export function useProjectNavRoutes(): AppRoute[] {
  const routes = useRoutes();
  const telemetry = useTelemetry();

  const isAssistantsEnabled = telemetry.isFeatureEnabled("assistants") ?? false;
  // Default true: opt-out via PostHog org-group targeting on `gram-deployments-page`.
  const isDeploymentsPageEnabled =
    telemetry.isFeatureEnabled("gram-deployments-page") ?? true;

  return [
    routes.home,
    routes.sources,
    routes.catalog,
    routes.playground,
    ...(isDeploymentsPageEnabled ? [routes.deployments] : []),
    routes.mcp,
    ...(isAssistantsEnabled ? [routes.assistants] : []),
    routes.clis,
    routes.plugins,
    routes.environments,
    routes.insights,
    routes.logs,
    routes.riskOverview,
    routes.policyCenter,
    routes.approvalRequests,
    routes.detectionRules,
    routes.settings,
  ];
}
