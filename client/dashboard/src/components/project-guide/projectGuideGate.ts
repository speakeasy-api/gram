export type ProjectGuideStatus = "pending" | "guide" | "dashboard";

export type ProjectGuideGateInput = {
  hasProjectSlug: boolean;
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

export function decideProjectGuideStatus(
  input: ProjectGuideGateInput,
): ProjectGuideStatus {
  if (!input.hasProjectSlug) return "dashboard";
  if (input.started) return "guide";
  if (input.serversPending || input.policiesPending) return "pending";
  if (input.serversError || input.policiesError) return "dashboard";
  if (input.hasServers || input.hasPolicies) return "dashboard";
  if (input.overviewPending) return "pending";
  if (input.overviewError || input.hasData) return "dashboard";
  return "guide";
}
