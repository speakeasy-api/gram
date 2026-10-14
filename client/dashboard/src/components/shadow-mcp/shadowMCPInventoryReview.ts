import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";

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
