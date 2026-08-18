export type ProjectGuideStatus = "pending" | "guide" | "dashboard";

export type ProjectGuideGateInput = {
  /** False on org-level routes, where there is no project to judge. */
  hasProjectSlug: boolean;
  rbacLoading: boolean;
  /** `org:admin`. Journey B cannot run without it: the observability plugin
   *  download mints a hooks-scoped org key and already refuses non-admins. */
  isAdmin: boolean;
  featuresPending: boolean;
  /** Both journeys end in something the user watches arrive, and nothing
   *  arrives without logs, so the guide would promise a win it cannot pay. */
  logsEnabled: boolean;
  dismissed: boolean;
  started: boolean;
  serversPending: boolean;
  serversError: boolean;
  hasServers: boolean;
  policiesPending: boolean;
  policiesError: boolean;
  hasPolicies: boolean;
  overviewPending: boolean;
  overviewError: boolean;
  hasData: boolean;
};

/**
 * Which of the two project-home surfaces to render.
 *
 * Order matters. Scope and entitlement come first and cost nothing (project
 * home already fetches both). A started run short-circuits every query: it is
 * the common path on repeat visits, and it is what keeps the guide on screen
 * after the guide's own steps have made the project non-empty. Only then does
 * the emptiness check run, cheapest signal first.
 *
 * "pending" means the caller should render neither surface yet. It is not
 * "dashboard by default": defaulting would flash the dashboard and then swap in
 * the guide a moment later on exactly the projects this feature exists for.
 */
export function decideProjectGuideStatus(
  input: ProjectGuideGateInput,
): ProjectGuideStatus {
  if (!input.hasProjectSlug) return "dashboard";
  if (input.rbacLoading || input.featuresPending) return "pending";
  if (!input.isAdmin) return "dashboard";
  if (!input.logsEnabled) return "dashboard";
  if (input.dismissed) return "dashboard";
  if (input.started) return "guide";

  if (input.serversPending || input.policiesPending) return "pending";
  // Absent data is NOT empty here either: a failed cheap check must not read
  // as "no servers, no policies" for an established project.
  if (input.serversError || input.policiesError) return "dashboard";
  if (input.hasServers || input.hasPolicies) return "dashboard";

  // A user can install the observability plugin, collect hook telemetry from
  // their own agent, and never create a server or policy. They have data and
  // should not get a beginner guide.
  if (input.overviewPending) return "pending";
  if (input.overviewError) return "dashboard";
  return input.hasData ? "dashboard" : "guide";
}
