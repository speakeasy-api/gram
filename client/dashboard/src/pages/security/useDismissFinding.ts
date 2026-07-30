import { useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useRiskMarkResultsFalsePositiveMutation } from "@gram/client/react-query/riskMarkResultsFalsePositive.js";
import { useRiskUnmarkResultsFalsePositiveMutation } from "@gram/client/react-query/riskUnmarkResultsFalsePositive.js";
import { invalidateAllRiskListDismissedResults } from "@gram/client/react-query/riskListDismissedResults.js";
import { invalidateAllRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { invalidateAllRiskRuleBreakdown } from "@gram/client/react-query/riskRuleBreakdown.js";
import { invalidateAllRiskUserBreakdown } from "@gram/client/react-query/riskUserBreakdown.js";
import { showUndoToast } from "@/lib/toast-undo";

// Mirrors maxFalsePositiveBatch in server/internal/risk/false_positive.go — a
// selection larger than the server's per-request cap is split into
// sequential requests here rather than surfaced as a hard UI limit.
const MAX_BATCH = 500;

function chunk<T>(items: T[], size: number): T[][] {
  const chunks: T[][] = [];
  for (let i = 0; i < items.length; i += size) {
    chunks.push(items.slice(i, i + size));
  }
  return chunks;
}

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
    void invalidateAllRiskRuleBreakdown(queryClient);
    void invalidateAllRiskUserBreakdown(queryClient);
  }, [queryClient]);

  const removeOptimistic = useCallback((ids: string[]) => {
    setOptimistic((prev) => {
      const next = new Set(prev);
      ids.forEach((id) => {
        next.delete(id);
      });
      return next;
    });
  }, []);

  const addOptimistic = useCallback((ids: string[]) => {
    setOptimistic((prev) => {
      const next = new Set(prev);
      ids.forEach((id) => {
        next.add(id);
      });
      return next;
    });
  }, []);

  const undo = useCallback(
    (ids: string[]) => {
      removeOptimistic(ids);
      unmarkMutation.mutate(
        {
          request: {
            unmarkRiskResultsFalsePositiveRequestBody: { resultIds: ids },
          },
        },
        {
          onSuccess: invalidateLists,
          onError: () => {
            // Put the ids back: the mark stayed in effect server-side, so the
            // optimistic hide must too, or the row would look restored while
            // still dismissed.
            addOptimistic(ids);
            toast.error("Failed to undo — the finding is still dismissed.");
          },
        },
      );
    },
    [unmarkMutation, invalidateLists, removeOptimistic, addOptimistic],
  );

  const dismiss = useCallback(
    (results: RiskResult[], reason?: string) => {
      const ids = results.map((r) => r.id);
      if (ids.length === 0) return;
      addOptimistic(ids);

      const batches = chunk(ids, MAX_BATCH);
      void Promise.allSettled(
        batches.map((batchIds) =>
          markMutation
            .mutateAsync({
              request: {
                markRiskResultsFalsePositiveRequestBody: {
                  resultIds: batchIds,
                  reason,
                },
              },
            })
            .then(
              () => ({ batchIds, ok: true as const }),
              () => ({ batchIds, ok: false as const }),
            ),
        ),
      ).then((settled) => {
        const outcomes = settled.map((s) =>
          s.status === "fulfilled"
            ? s.value
            : { batchIds: [] as string[], ok: false as const },
        );
        const failedIds = outcomes
          .filter((o) => !o.ok)
          .flatMap((o) => o.batchIds);
        const succeededIds = outcomes
          .filter((o) => o.ok)
          .flatMap((o) => o.batchIds);

        if (failedIds.length > 0) {
          removeOptimistic(failedIds);
          toast.error(
            `Failed to mark ${failedIds.length} finding${failedIds.length === 1 ? "" : "s"} as false positive.`,
          );
        }
        if (succeededIds.length > 0) {
          invalidateLists();
          // Undo only becomes available once the mark has actually
          // succeeded — offering it earlier would let an immediate click
          // race the outstanding mark request.
          showUndoToast(`Marked ${succeededIds.length} as false positive`, () =>
            undo(succeededIds),
          );
        }
      });
    },
    [markMutation, invalidateLists, undo, addOptimistic, removeOptimistic],
  );

  const isOptimisticallyDismissed = useCallback(
    (id: string) => optimistic.has(id),
    [optimistic],
  );

  return {
    dismiss,
    isOptimisticallyDismissed,
  };
}
