import { accessOf } from "@/components/shadow-ai/access";
import { matchesReviewFilter } from "@/components/shadow-mcp/shadowMCPInventoryReview";
import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";

/** Detected tools an admin could decide about but has not: enforceable, with
 *  no decision on file. Local models and tools with no gateway identity are
 *  left out, since no decision about them would do anything. */
export function countUndecidedAITools(
  detections: readonly AIDetection[],
): number {
  return detections.filter((detection) => {
    const access = accessOf(detection);
    return access.enforceable && access.decision === "unreviewed";
  }).length;
}

/** Servers nobody has approved or denied: no review on file, or a request
 *  still waiting with no earlier decision standing behind it. A reopened
 *  request keeps enforcing its prior decision until re-decided, so it is not
 *  part of the backlog. */
export function countUndecidedMCPServers(
  servers: readonly ShadowMCPInventoryServer[],
): number {
  return servers.filter(
    (server) =>
      matchesReviewFilter(server, "none") ||
      (matchesReviewFilter(server, "requested") &&
        server.approvalRequest?.standingDecision === undefined),
  ).length;
}
