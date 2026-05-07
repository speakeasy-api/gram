import type {
  ShadowMCPApprovalRequest,
  ShadowMCPDecision,
  ShadowMCPMatchBreadth,
  ShadowMCPRequestStatus,
  ShadowMCPServerListEntry,
} from "./shadow-mcp-types";

export function getMatchBreadthLabel(matchBreadth: ShadowMCPMatchBreadth) {
  return matchBreadth === "full_url" ? "Full URL" : "URL host";
}

export function getMatchValue(
  entry: Pick<ShadowMCPServerListEntry, "evidence" | "matchBreadth">,
) {
  return entry.matchBreadth === "full_url"
    ? entry.evidence.fullUrl
    : entry.evidence.urlHost;
}

export function formatShortDate(value: string) {
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  }).format(new Date(value));
}

export function getRequestStatusLabel(status: ShadowMCPRequestStatus) {
  switch (status) {
    case "requested":
      return "Requested";
    case "approved":
      return "Approved";
    case "denied":
      return "Denied";
  }
}

export function getDecisionLabel(decision: ShadowMCPDecision) {
  return decision === "allowed" ? "Allowed" : "Denied";
}

export function getShadowMCPSummary({
  requests,
  entries,
}: {
  requests: ShadowMCPApprovalRequest[];
  entries: ShadowMCPServerListEntry[];
}) {
  const requested = requests.filter((r) => r.status === "requested").length;
  const approved = requests.filter((r) => r.status === "approved").length;
  const deniedRequests = requests.filter((r) => r.status === "denied").length;
  const allowedServers = entries.filter((e) => e.decision === "allowed");
  const deniedServers = entries.filter((e) => e.decision === "denied");
  const roleGrantCount = new Set(allowedServers.flatMap((e) => e.roleIds)).size;

  return {
    requested,
    approved,
    deniedRequests,
    allowedServers: allowedServers.length,
    deniedServers: deniedServers.length,
    roleGrantCount,
  };
}
