import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";

/**
 * A seen-time is real only when telemetry produced it: synthesized
 * review-only rows carry the zero time, which must read as never observed
 * rather than as January of year one.
 */
export function observedDate(date: Date | undefined): Date | undefined {
  if (!date || date.getTime() <= 0) return undefined;
  return date;
}

/**
 * Pending decisions sort first; the rest follow the review lifecycle. An
 * unreviewed dossier is a storage detail, not a state — it ranks with "no
 * review".
 */
export function reviewSortRank(server: ShadowMCPInventoryServer): number {
  switch (server.approvalRequest?.status) {
    case "requested":
      return 0;
    case "approved":
    case "denied":
    // Superseded ranks with the decided states: it is settled review
    // history — an admin deliberately displaced the decision — not
    // something waiting on anyone.
    case "superseded":
      return 1;
    case "unreviewed":
    case undefined:
      return 2;
  }
}

export function matchesReviewFilter(
  server: ShadowMCPInventoryServer,
  review: string | undefined,
): boolean {
  if (!review) return true;
  if (review === "none") {
    return (
      server.approvalRequest === undefined ||
      server.approvalRequest.status === "unreviewed"
    );
  }
  return server.approvalRequest?.status === review;
}
