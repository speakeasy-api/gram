import type { JourneyStatus } from "@/components/project-guide/journeys";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";

/**
 * Journey A's own artifact: a server whose backend is a catalog remote MCP
 * server. Toolset-, tunnel-, and unproxied-backed servers are other features.
 */
export function hasCatalogBackedServer(
  servers: McpServer[] | undefined,
): boolean {
  return (servers ?? []).some((server) => Boolean(server.remoteMcpServerId));
}

/**
 * Journey B's own artifact. `gitleaks` is how the `secrets` category is stored
 * on a policy's `sources` (see policyToCategories in pages/security).
 */
export function hasBlockingSecretsPolicy(
  policies: RiskPolicy[] | undefined,
): boolean {
  return (policies ?? []).some(
    (policy) =>
      policy.enabled &&
      policy.action === "block" &&
      policy.sources.includes("gitleaks"),
  );
}

/**
 * Card status from backend state alone, never from local flags, so a user who
 * did some of this yesterday is credited for it. A recorded win outranks a
 * missing start signal: someone who blocked a prompt and later deleted the
 * policy has still had the win.
 */
export function deriveJourneyStatus(signals: {
  startSignal: boolean;
  winSignal: boolean;
}): JourneyStatus {
  if (signals.winSignal) return "done";
  return signals.startSignal ? "in-progress" : "not-started";
}

/**
 * Which step an expanded card opens on. Resolution is card-level for now: the
 * middle steps (server added to the Default plugin, observability plugin
 * installed) have no cheap signal, and land with the journey bodies.
 */
export function firstIncompleteStepIndex(
  status: JourneyStatus,
  stepCount: number,
): number {
  if (status === "done") return stepCount;
  return status === "not-started" ? 0 : 1;
}
