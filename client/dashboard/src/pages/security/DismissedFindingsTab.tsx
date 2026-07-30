import { Type } from "@/components/ui/type";
import { MoreActions, type Action } from "@/components/ui/more-actions";
import { type Column, Table } from "@speakeasy-api/moonshine";
import { format } from "date-fns";
import type { JSX } from "react";
import {
  undoDismiss,
  useDismissedFindings,
  type DismissedFinding,
} from "./false-positive-demo-store";
import { CategoryLabel, RuleLabel } from "./risk-ui";

// TEMPORARY UX-DEMO SCAFFOLDING for AIS-321 — reads the client-side demo store
// instead of a real `risk.listDismissedResults` endpoint. See
// false-positive-demo-store.ts for the removal plan.
export function DismissedFindingsTab(): JSX.Element {
  const findings = useDismissedFindings();

  const columns: Column<DismissedFinding>[] = [
    {
      key: "finding",
      header: "Finding",
      width: "2fr",
      render: ({ result }) => (
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
      render: ({ result }) => (
        <Type className="truncate" small>
          {result.chatTitle ?? "Untitled"}
        </Type>
      ),
    },
    {
      key: "dismissedAt",
      header: "Dismissed",
      width: "0.9fr",
      render: ({ dismissedAt }) => (
        <Type className="text-muted-foreground" small>
          {format(dismissedAt, "MMM d, yyyy h:mm a")}
        </Type>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "0.3fr",
      render: ({ result }) => (
        <div onClick={(e) => e.stopPropagation()}>
          <MoreActions
            actions={
              [
                {
                  label: "Undo",
                  onClick: () => undoDismiss(result.id),
                },
              ] satisfies Action[]
            }
          />
        </div>
      ),
    },
  ];

  if (findings.length === 0) {
    return <DismissedEmptyState />;
  }

  return (
    <Table columns={columns} data={findings} rowKey={(f) => f.result.id} />
  );
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
