import { useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useRiskMarkResultsFalsePositiveMutation } from "@gram/client/react-query/riskMarkResultsFalsePositive.js";
import { useRiskUnmarkResultsFalsePositiveMutation } from "@gram/client/react-query/riskUnmarkResultsFalsePositive.js";
import { invalidateAllRiskListDismissedResults } from "@gram/client/react-query/riskListDismissedResults.js";
import { invalidateAllRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { showUndoToast } from "@/lib/toast-undo";

/** Bulk/single mark-false-positive with optimistic hide + undo toast, shared
 * by the risk overview category table, the risk events log, and the chat
 * session risk popover. Real mutations, not the AIS-321 UX-demo store: a
 * dismiss removes the result from every listRiskResults-backed surface
 * server-side, so this hook only needs to bridge the gap between "mutation
 * fired" and "the list query has refetched without it". */
export function useDismissFinding(): {
  dismiss: (results: RiskResult[], reason?: string) => void;
  isOptimisticallyDismissed: (id: string) => boolean;
} {
  const queryClient = useQueryClient();
  const [optimistic, setOptimistic] = useState<Set<string>>(new Set());

  const markMutation = useRiskMarkResultsFalsePositiveMutation();
  const unmarkMutation = useRiskUnmarkResultsFalsePositiveMutation();

  const invalidateLists = useCallback(() => {
    // RiskEvents.tsx and RiskOverviewCategoryDetail.tsx query listRiskResults
    // directly via useInfiniteQuery (not the generated hook), under queryKeys
    // starting with this prefix — see those files for the exact keys.
    void queryClient.invalidateQueries({
      queryKey: ["risk", "results", "list"],
    });
    void invalidateAllRiskListDismissedResults(queryClient);
    void invalidateAllRiskOverview(queryClient);
  }, [queryClient]);

  const undo = useCallback(
    (ids: string[]) => {
      setOptimistic((prev) => {
        const next = new Set(prev);
        ids.forEach((id) => {
          next.delete(id);
        });
        return next;
      });
      unmarkMutation.mutate(
        {
          request: {
            unmarkRiskResultsFalsePositiveRequestBody: { resultIds: ids },
          },
        },
        { onSuccess: invalidateLists },
      );
    },
    [unmarkMutation, invalidateLists],
  );

  const dismiss = useCallback(
    (results: RiskResult[], reason?: string) => {
      const ids = results.map((r) => r.id);
      if (ids.length === 0) return;
      setOptimistic((prev) => {
        const next = new Set(prev);
        ids.forEach((id) => {
          next.add(id);
        });
        return next;
      });
      markMutation.mutate(
        {
          request: {
            markRiskResultsFalsePositiveRequestBody: { resultIds: ids, reason },
          },
        },
        { onSuccess: invalidateLists },
      );
      showUndoToast(`Marked ${ids.length} as false positive`, () => undo(ids));
    },
    [markMutation, invalidateLists, undo],
  );

  return {
    dismiss,
    isOptimisticallyDismissed: (id: string) => optimistic.has(id),
  };
}
