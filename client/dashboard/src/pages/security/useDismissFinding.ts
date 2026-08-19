import { useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useRiskMarkResultsFalsePositiveMutation } from "@gram/client/react-query/riskMarkResultsFalsePositive.js";
import { useRiskUnmarkResultsFalsePositiveMutation } from "@gram/client/react-query/riskUnmarkResultsFalsePositive.js";
import { invalidateAllRiskListDismissedResults } from "@gram/client/react-query/riskListDismissedResults.js";
import { invalidateAllRiskListResults } from "@gram/client/react-query/riskListResults.js";
import { invalidateAllRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { invalidateAllRiskRuleBreakdown } from "@gram/client/react-query/riskRuleBreakdown.js";
import { invalidateAllRiskSignals } from "@gram/client/react-query/riskSignals.js";
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

/** Runs `mutateAsync` once per MAX_BATCH-sized chunk of `ids`, one batch at a
 * time (not concurrently — a selection of several thousand ids should not
 * turn into a burst of concurrent DB/audit writes). A failed batch doesn't
 * stop the remaining ones; the caller gets back which ids landed on each
 * side so it can roll back optimistic state and report partial failure. */
async function runBatched(
  ids: string[],
  mutateAsync: (batchIds: string[]) => Promise<unknown>,
): Promise<{ succeededIds: string[]; failedIds: string[] }> {
  const succeededIds: string[] = [];
  const failedIds: string[] = [];
  for (const batchIds of chunk(ids, MAX_BATCH)) {
    try {
      await mutateAsync(batchIds);
      succeededIds.push(...batchIds);
    } catch {
      failedIds.push(...batchIds);
    }
  }
  return { succeededIds, failedIds };
}

/** Bulk/single suppression with optimistic hide + undo toast, shared by the
 * risk overview category table, the risk events log, and the chat session
 * risk popover. Real mutations, not the AIS-321 UX-demo store: suppressing a
 * finding removes it from every listRiskResults-backed surface server-side,
 * so this hook only needs to bridge the gap between "mutation fired" and "the
 * list query has refetched without it".
 *
 * `restore` is the same unmark call reached from the other direction — the
 * Watchdog's Suppressed section, where the finding is already hidden and the
 * user is putting it back — so it reports as a first-class action rather than
 * as an undo of something that just happened. It resolves to whether anything
 * actually came back, so a caller showing the finding (the suppressed-finding
 * drawer) can close only once the restore landed. */
export function useDismissFinding(): {
  dismiss: (results: RiskResult[], reason?: string) => void;
  restore: (ids: string[]) => Promise<boolean>;
  isOptimisticallyDismissed: (id: string) => boolean;
  isOptimisticallyRestored: (id: string) => boolean;
} {
  const queryClient = useQueryClient();
  const [optimistic, setOptimistic] = useState<Set<string>>(new Set());
  // The mirror image of `optimistic`, and deliberately a separate set: an id
  // in `optimistic` is hidden from the surfaces that list *live* findings,
  // while an id here is hidden from the one surface that lists *suppressed*
  // ones. Folding them together would make a restore hide the finding from the
  // active listings it is on its way back into.
  const [optimisticRestored, setOptimisticRestored] = useState<Set<string>>(
    new Set(),
  );

  const markMutation = useRiskMarkResultsFalsePositiveMutation();
  const unmarkMutation = useRiskUnmarkResultsFalsePositiveMutation();

  const invalidateLists = useCallback(() => {
    // RiskEvents.tsx and RiskOverviewCategoryDetail.tsx each query results
    // directly via useInfiniteQuery (not a generated hook), under their own
    // queryKey under this shared ["risk", "results", ...] prefix (e.g.
    // ["risk","results","list"]). Invalidate the whole prefix rather than each
    // exact key — suppressing from one surface (e.g. Risk Events) must be
    // reflected on every other surface, and matching each new custom key
    // one-by-one here is exactly the kind of thing that's easy to add a
    // surface and forget to wire up.
    void queryClient.invalidateQueries({
      queryKey: ["risk", "results"],
    });
    void invalidateAllRiskListDismissedResults(queryClient);
    // The chat transcript's ChatDetailPanel reads findings through the
    // *generated* useRiskListResults hook (a separate ["@gram/client",
    // "results", "list", ...] key namespace the prefix above never touches),
    // so a tool-call-section dismiss left the transcript showing a finding
    // as still flagged until a manual reload.
    void invalidateAllRiskListResults(queryClient);
    void invalidateAllRiskOverview(queryClient);
    void invalidateAllRiskRuleBreakdown(queryClient);
    void invalidateAllRiskUserBreakdown(queryClient);
    // The Watchdog page clusters the same findings into signals.
    void invalidateAllRiskSignals(queryClient);
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

  const unsuppress = useCallback(
    (ids: string[]) =>
      runBatched(ids, (batchIds) =>
        unmarkMutation.mutateAsync({
          request: {
            unmarkRiskResultsFalsePositiveRequestBody: { resultIds: batchIds },
          },
        }),
      ),
    [unmarkMutation],
  );

  const undo = useCallback(
    (ids: string[]) => {
      removeOptimistic(ids);
      void unsuppress(ids).then(({ succeededIds, failedIds }) => {
        if (succeededIds.length > 0) {
          invalidateLists();
        }
        if (failedIds.length > 0) {
          // Put the failed ids back: the suppression stayed in effect
          // server-side, so the optimistic hide must too, or the row would
          // look restored while still suppressed.
          addOptimistic(failedIds);
          toast.error("Failed to undo — the finding is still suppressed.");
        }
      });
    },
    [unsuppress, invalidateLists, removeOptimistic, addOptimistic],
  );

  const restore = useCallback(
    async (ids: string[]): Promise<boolean> => {
      if (ids.length === 0) return false;
      // Hide the rows before the request goes out. The suppressed listing is
      // served from the ClickHouse mirror, which lags the write, so the
      // refetch this restore triggers can still come back carrying the rows
      // that were just restored — without this they would reappear and stay
      // until some later refetch happened to land after the mirror caught up.
      setOptimisticRestored((prev) => new Set([...prev, ...ids]));
      const { succeededIds, failedIds } = await unsuppress(ids);
      if (succeededIds.length > 0) {
        invalidateLists();
        toast.success(
          `Restored ${succeededIds.length} ${succeededIds.length === 1 ? "finding" : "findings"}`,
        );
      }
      if (failedIds.length > 0) {
        // Still suppressed server-side, so the row belongs back on the list.
        setOptimisticRestored((prev) => {
          const next = new Set(prev);
          failedIds.forEach((id) => {
            next.delete(id);
          });
          return next;
        });
        toast.error(
          `Failed to restore ${failedIds.length} ${failedIds.length === 1 ? "finding" : "findings"}.`,
        );
      }
      return succeededIds.length > 0;
    },
    [unsuppress, invalidateLists],
  );

  const dismiss = useCallback(
    (results: RiskResult[], reason?: string) => {
      const ids = results.map((r) => r.id);
      if (ids.length === 0) return;
      addOptimistic(ids);

      void runBatched(ids, (batchIds) =>
        markMutation.mutateAsync({
          request: {
            markRiskResultsFalsePositiveRequestBody: {
              resultIds: batchIds,
              reason,
            },
          },
        }),
      ).then(({ succeededIds, failedIds }) => {
        if (failedIds.length > 0) {
          removeOptimistic(failedIds);
          toast.error(
            `Failed to suppress ${failedIds.length} finding${failedIds.length === 1 ? "" : "s"}.`,
          );
        }
        if (succeededIds.length > 0) {
          invalidateLists();
          // Undo only becomes available once the suppression has actually
          // succeeded — offering it earlier would let an immediate click
          // race the outstanding request.
          showUndoToast(
            `Suppressed ${succeededIds.length} ${succeededIds.length === 1 ? "finding" : "findings"}`,
            () => undo(succeededIds),
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

  const isOptimisticallyRestored = useCallback(
    (id: string) => optimisticRestored.has(id),
    [optimisticRestored],
  );

  return {
    dismiss,
    restore,
    isOptimisticallyDismissed,
    isOptimisticallyRestored,
  };
}
