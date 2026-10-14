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
 * A legacy policy-bypass request is a pending ask too: the server counts only
 * the requests still waiting against an enabled blocking policy, so a row
 * with one is awaiting a decision whatever its dossier says.
 */
function hasPendingBypassRequest(server: ShadowMCPInventoryServer): boolean {
  return server.requestCount > 0;
}

/**
 * Pending decisions sort first; the rest follow the review lifecycle. An
 * unreviewed dossier is a storage detail, not a state — it ranks with "no
 * review".
 */
export function reviewSortRank(server: ShadowMCPInventoryServer): number {
  if (hasPendingBypassRequest(server)) return 0;
  switch (server.approvalRequest?.status) {
    case "requested":
      return 0;
    case "approved":
    case "denied":
    // Superseded is settled review history, not something waiting on anyone.
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
  if (review === "requested") {
    return (
      server.approvalRequest?.status === "requested" ||
      hasPendingBypassRequest(server)
    );
  }
  if (review === "none") {
    return (
      !hasPendingBypassRequest(server) &&
      (server.approvalRequest === undefined ||
        server.approvalRequest.status === "unreviewed")
    );
  }
  return server.approvalRequest?.status === review;
}
