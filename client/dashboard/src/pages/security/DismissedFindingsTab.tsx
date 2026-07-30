import { Type } from "@/components/ui/type";
import { MoreActions, type Action } from "@/components/ui/more-actions";
import { type Column, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { format } from "date-fns";
import type { JSX } from "react";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import {
  invalidateAllRiskListDismissedResults,
  useRiskListDismissedResults,
} from "@gram/client/react-query/riskListDismissedResults.js";
import { useRiskUnmarkResultsFalsePositiveMutation } from "@gram/client/react-query/riskUnmarkResultsFalsePositive.js";
import { invalidateAllRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { CategoryLabel, RuleLabel } from "./risk-ui";

export function DismissedFindingsTab(): JSX.Element {
  const queryClient = useQueryClient();
  const { data, isLoading } = useRiskListDismissedResults({});
  const findings = data?.results ?? [];

  const unmarkMutation = useRiskUnmarkResultsFalsePositiveMutation({
    onSuccess: () => {
      void invalidateAllRiskListDismissedResults(queryClient);
      void queryClient.invalidateQueries({
        queryKey: ["risk", "results", "list"],
      });
      void invalidateAllRiskOverview(queryClient);
    },
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
        <Type className="truncate" small>
          {result.chatTitle ?? "Untitled"}
        </Type>
      ),
    },
    {
      key: "dismissedAt",
      header: "Dismissed",
      width: "0.9fr",
      render: (result) => (
        <Type className="text-muted-foreground" small>
          {result.falsePositiveAt
            ? format(result.falsePositiveAt, "MMM d, yyyy h:mm a")
            : "-"}
        </Type>
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

  if (isLoading) {
    return (
      <Type className="text-muted-foreground">Loading dismissed findings…</Type>
    );
  }

  if (findings.length === 0) {
    return <DismissedEmptyState />;
  }

  return <Table columns={columns} data={findings} rowKey={(f) => f.id} />;
}

function DismissedEmptyState() {
  return (
    <div className="bg-background flex h-[360px] w-full flex-col items-center justify-center gap-4 rounded-xl border">
      <div className="space-y-1 text-center">
        <Type className="font-medium">No dismissed findings yet</Type>
        <Type small muted>
          Findings marked as false positive from Risk Events, Risk Overview, or
          a chat session will show up here — undo any of them at any time.
        </Type>
      </div>
    </div>
  );
}
