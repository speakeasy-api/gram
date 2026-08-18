import { Text } from "@/components/ui/Text";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Button } from "@/components/ui/Button";
import { type Column, Table } from "@/components/ui/Table";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { format } from "date-fns";
import { toast } from "sonner";
import type { JSX } from "react";
import { useMemo } from "react";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { invalidateAllRiskListDismissedResults } from "@gram/client/react-query/riskListDismissedResults.js";
import { invalidateAllRiskListResults } from "@gram/client/react-query/riskListResults.js";
import { useRiskUnmarkResultsFalsePositiveMutation } from "@gram/client/react-query/riskUnmarkResultsFalsePositive.js";
import { invalidateAllRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { invalidateAllRiskRuleBreakdown } from "@gram/client/react-query/riskRuleBreakdown.js";
import { invalidateAllRiskUserBreakdown } from "@gram/client/react-query/riskUserBreakdown.js";
import { useSdkClient } from "@/contexts/Sdk";
import { CategoryLabel, RuleLabel } from "./risk-ui";

export function DismissedFindingsTab(): JSX.Element {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  const resultsQuery = useInfiniteQuery({
    queryKey: ["risk", "results", "list-dismissed"],
    queryFn: async ({ pageParam }) => {
      return client.risk.results.listDismissed({
        cursor: pageParam,
        limit: 50,
      });
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
  });

  const findings = useMemo(
    () => resultsQuery.data?.pages.flatMap((p) => p.results) ?? [],
    [resultsQuery.data],
  );

  const invalidateLists = () => {
    // This component's own results come from a plain useInfiniteQuery (see
    // above) under ["risk","results","list-dismissed"], not the generated
    // hook — invalidateAllRiskListDismissedResults only covers the generated
    // hook's own key namespace and never touches this one. Invalidate the
    // whole ["risk","results"] prefix (shared with RiskEvents.tsx and
    // RiskOverviewCategoryDetail.tsx's own custom queries) rather than this
    // exact key alone, matching useDismissFinding.ts's invalidateLists.
    void queryClient.invalidateQueries({
      queryKey: ["risk", "results"],
    });
    void invalidateAllRiskListDismissedResults(queryClient);
    void invalidateAllRiskListResults(queryClient);
    void invalidateAllRiskOverview(queryClient);
    void invalidateAllRiskRuleBreakdown(queryClient);
    void invalidateAllRiskUserBreakdown(queryClient);
  };

  const unmarkMutation = useRiskUnmarkResultsFalsePositiveMutation({
    onSuccess: invalidateLists,
    onError: () =>
      toast.error("Failed to undo — the finding is still dismissed."),
  });

  const columns: Column<RiskResult>[] = [
    {
      key: "finding",
      header: "Finding",
      width: "2fr",
      render: (result) => (
        <div className="flex min-w-0 flex-col gap-1">
          <CategoryLabel source={result.source} ruleId={result.ruleId} />
          <RuleLabel source={result.source} ruleId={result.ruleId} />
        </div>
      ),
    },
    {
      key: "session",
      header: "Session",
      width: "1.2fr",
      render: (result) => (
        <Text className="truncate" small>
          {result.chatTitle ?? "Untitled"}
        </Text>
      ),
    },
    {
      key: "dismissedAt",
      header: "Dismissed",
      width: "0.9fr",
      render: (result) => (
        <Text className="text-muted-foreground" small>
          {result.falsePositiveAt
            ? format(result.falsePositiveAt, "MMM d, yyyy h:mm a")
            : "-"}
        </Text>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "0.3fr",
      render: (result) => (
        <div onClick={(e) => e.stopPropagation()}>
          <MoreActions
            actions={
              [
                {
                  label: "Undo",
                  onClick: () =>
                    unmarkMutation.mutate({
                      request: {
                        unmarkRiskResultsFalsePositiveRequestBody: {
                          resultIds: [result.id],
                        },
                      },
                    }),
                },
              ] satisfies Action[]
            }
          />
        </div>
      ),
    },
  ];

  if (resultsQuery.isLoading) {
    return (
      <Text className="text-muted-foreground">Loading dismissed findings…</Text>
    );
  }

  if (resultsQuery.isError) {
    return (
      <div className="bg-background flex h-[360px] w-full flex-col items-center justify-center gap-4 border">
        <div className="space-y-1 text-center">
          <Text className="font-medium">Couldn't load dismissed findings</Text>
          <Text small muted>
            Something went wrong fetching this list.
          </Text>
        </div>
        <Button variant="secondary" onClick={() => void resultsQuery.refetch()}>
          Retry
        </Button>
      </div>
    );
  }

  if (findings.length === 0) {
    return <DismissedEmptyState />;
  }

  return (
    <div className="flex flex-col gap-3">
      <Table columns={columns} data={findings} rowKey={(f) => f.id} />
      {resultsQuery.hasNextPage && (
        <Button
          variant="secondary"
          className="self-center"
          disabled={resultsQuery.isFetchingNextPage}
          onClick={() => void resultsQuery.fetchNextPage()}
        >
          {resultsQuery.isFetchingNextPage ? "Loading…" : "Load more"}
        </Button>
      )}
    </div>
  );
}

function DismissedEmptyState() {
  return (
    <div className="bg-background flex h-[360px] w-full flex-col items-center justify-center gap-4 border">
      <div className="space-y-1 text-center">
        <Text className="font-medium">No dismissed findings yet</Text>
        <Text small muted>
          Findings marked as false positive from Risk Events, Risk Overview, or
          a chat session will show up here — undo any of them at any time.
        </Text>
      </div>
    </div>
  );
}
