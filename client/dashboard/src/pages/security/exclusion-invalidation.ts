import { invalidateAllListChats } from "@gram/client/react-query/listChats.js";
import { invalidateAllRiskListExclusions } from "@gram/client/react-query/riskListExclusions.js";
import { invalidateAllRiskListResults } from "@gram/client/react-query/riskListResults.js";
import { invalidateAllRiskListResultsByChat } from "@gram/client/react-query/riskListResultsByChat.js";
import { invalidateAllRiskListResultsForAgent } from "@gram/client/react-query/riskListResultsForAgent.js";
import { invalidateAllRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { invalidateAllRiskSignals } from "@gram/client/react-query/riskSignals.js";
import type { QueryClient } from "@tanstack/react-query";

/**
 * Saving an exclusion suppresses/restores findings retroactively, so refresh
 * the exclusion list AND every risk-results surface (chat detail, agent,
 * overview, watchdog signals) so stale findings disappear without a manual
 * reload. Note the server applies the exclusion asynchronously (Temporal
 * reconcile), so the refetched results lag; hosts that need instant feedback
 * hide the originating finding optimistically. Shared by the exclusion editor
 * and every surface that creates exclusions without it (e.g. the Watchdog
 * bulk action) so they all refresh the exact same set.
 */
export function invalidateExclusionSurfaces(
  queryClient: QueryClient,
): Promise<unknown> {
  return Promise.all([
    invalidateAllRiskListExclusions(queryClient),
    invalidateAllRiskListResults(queryClient),
    invalidateAllRiskListResultsByChat(queryClient),
    invalidateAllRiskListResultsForAgent(queryClient),
    invalidateAllRiskOverview(queryClient),
    // The Watchdog page clusters the same findings into signals.
    invalidateAllRiskSignals(queryClient),
    // The Agent Sessions list shows per-session risk counts, so refresh it too
    // (lags the async reconcile like the other surfaces).
    invalidateAllListChats(queryClient),
  ]);
}
