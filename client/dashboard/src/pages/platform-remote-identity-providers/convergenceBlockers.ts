import type { IssuerConvergenceCandidate } from "@gram/client/models/components/issuerconvergencecandidate.js";

// candidateBlockerSummary is the one-line status for a candidate row: why it
// cannot be consolidated, what would change if it were, or that nothing is known
// to stand in the way.
//
// The listing carries only the blockers that are pure functions of the two
// issuer records. Detecting a conflicting MCP-server binding takes per-candidate
// queries against every tenant on the target, so it is left to the preflight the
// dialog runs. That is why a clean row promises no blockers "found" rather than
// none existing: the dialog is the authority, and it can still refuse.
export function candidateBlockerSummary(
  candidate: IssuerConvergenceCandidate,
): string {
  if (candidate.endpointMismatches.length > 0) {
    return `Different authorization server (${candidate.endpointMismatches.join(", ")} differ)`;
  }

  if (candidate.warnings.length > 0) {
    return candidate.warnings.join(" ");
  }

  return "No blockers found";
}

// candidateIsBlocked reports whether the listing already knows this candidate
// cannot be migrated, which is exactly when an endpoint mismatch is present.
// Warnings never block: the target's values simply become authoritative.
export function candidateIsBlocked(
  candidate: IssuerConvergenceCandidate,
): boolean {
  return candidate.endpointMismatches.length > 0;
}

// candidateOwnerLabel names the organization a candidate belongs to, falling
// back to the raw id when WorkOS metadata has not synced and to a plain
// description when the row predates the organization_id column entirely. A blank
// cell would read as a rendering bug rather than as missing data.
export function candidateOwnerLabel(
  candidate: IssuerConvergenceCandidate,
): string {
  if (candidate.organizationName !== "") {
    return candidate.organizationName;
  }

  if (candidate.organizationId !== "") {
    return candidate.organizationId;
  }

  return "Unknown organization";
}
