import type { UserSessionWorkload } from "@gram/client/models/components/usersessionworkload.js";

/**
 * How the workload's issuer reads on screen. The id is the fallback: an issuer
 * that has been deleted, or that belongs to another project, has no name this
 * reader may see, but the session still names it.
 */
export function workloadIssuerLabel(workload: UserSessionWorkload): string {
  return workload.workloadIssuerName ?? `Issuer ${workload.workloadIssuerId}`;
}

/**
 * The assigned agent as a short phrase, including its state when that state
 * already refuses the workload's requests.
 */
export function workloadAgentLabel(workload: UserSessionWorkload): string {
  if (!workload.agentId) return "No agent assigned";
  const name = workload.agentName ?? workload.agentId;
  switch (workload.agentStatus) {
    case "suspended":
      return `Agent ${name} (suspended)`;
    case "revoked":
      return `Agent ${name} (revoked)`;
    case "active":
    case undefined:
      return `Agent ${name}`;
  }
}
