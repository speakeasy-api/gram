import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { AIToolIcon } from "@/components/ai-tools/AIToolIcon";
import { SignalBadges } from "@/components/shadow-ai/SignalBadges";
import { accessOf } from "@/components/shadow-ai/access";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import { invalidateAllAiDetectionUsers } from "@gram/client/react-query/aiDetectionUsers.js";
import { invalidateAllAiDetections } from "@gram/client/react-query/aiDetections.js";
import { useSetAIToolDecisionMutation } from "@gram/client/react-query/setAIToolDecision.js";
import { useQueryClient } from "@tanstack/react-query";
import { CircleDashed } from "lucide-react";
import type { ReactNode } from "react";
import { toast } from "sonner";
import { aiToolDecisionOf, nextAIToolDecision } from "./ai-tool-decision";

const DECISION_OPTIONS: { value: "approved" | "blocked"; label: string }[] = [
  { value: "approved", label: "Approve" },
  { value: "blocked", label: "Disallow" },
];

const INVENTORY_ONLY_REASON =
  "This tool never identifies itself to the MCP gateway, so a decision about it would have nothing to enforce. It is listed for inventory.";

/**
 * Approve | Disallow for one tool, written straight to the organization's
 * decision for it. A tool with neither pressed is unreviewed, shown by the
 * dashed marker beside the control rather than by a third option; pressing
 * the option in force withdraws it.
 */
function AIToolDecisionControl({
  detection,
}: {
  detection: AIDetection;
}): JSX.Element {
  const queryClient = useQueryClient();
  const current = aiToolDecisionOf(detection);
  const mutation = useSetAIToolDecisionMutation({
    // Returned so `isPending` holds until the rows refetch: `current` is
    // read from the row, and a press before the refetch would act on the
    // decision that was just replaced.
    onSuccess: () =>
      Promise.all([
        invalidateAllAiDetections(queryClient),
        invalidateAllAiDetectionUsers(queryClient),
      ]),
    onError: () => {
      toast.error(
        `The decision for ${detection.displayName} could not be saved. Try again.`,
      );
    },
  });

  if (current === "inventory_only") {
    return (
      <SimpleTooltip tooltip={INVENTORY_ONLY_REASON}>
        <Text small muted>
          Inventory only
        </Text>
      </SimpleTooltip>
    );
  }

  const record = (clicked: "approved" | "blocked") => {
    // `current` only moves once the refetch lands, so a second press inside
    // that window would re-record the option instead of withdrawing it.
    if (mutation.isPending) return;
    mutation.mutate({
      request: {
        setAIToolDecisionRequestBody: {
          targetId: detection.targetId,
          decision: nextAIToolDecision(current, clicked),
          // An omitted rationale is stored as empty, so carry the one on file.
          rationale: accessOf(detection).rationale,
        },
      },
    });
  };

  return (
    <div className="flex items-center gap-3">
      <SegmentedControl
        ariaLabel={`Access decision for ${detection.displayName}`}
        className="h-7"
        value={current === "unreviewed" ? null : current}
        onChange={record}
        options={DECISION_OPTIONS}
      />
      {current === "unreviewed" ? (
        <SimpleTooltip tooltip="No decision recorded. The tool connects until you disallow it.">
          <span className="text-muted-foreground inline-flex items-center gap-1 text-xs">
            <CircleDashed
              className="text-default-warning h-3.5 w-3.5"
              aria-hidden="true"
            />
            Unreviewed
          </span>
        </SimpleTooltip>
      ) : null}
    </div>
  );
}

const COLUMNS: Column<AIDetection>[] = [
  {
    key: "tool",
    header: "Tool",
    width: "1.6fr",
    render: (detection) => (
      <div className="flex min-w-0 items-center gap-3">
        <AIToolIcon
          targetId={detection.targetId}
          displayName={detection.displayName}
          className="h-6 w-6 shrink-0"
        />
        <div className="flex min-w-0 flex-col gap-1">
          <Text className="truncate font-medium">{detection.displayName}</Text>
          <SignalBadges signals={detection.signals} />
        </div>
      </div>
    ),
  },
  {
    key: "reach",
    header: "Users · devices",
    width: "0.8fr",
    render: (detection) => (
      <Text small className="whitespace-nowrap tabular-nums">
        {detection.userCount} · {detection.deviceCount}
      </Text>
    ),
  },
  {
    key: "lastSeen",
    header: "Last seen",
    width: "0.7fr",
    render: (detection) => (
      <Text small className="whitespace-nowrap">
        {formatShortDate(detection.lastSeen)}
      </Text>
    ),
  },
  {
    key: "decision",
    header: "Decision",
    width: "1.4fr",
    render: (detection) => (
      <div onClick={(event) => event.stopPropagation()}>
        <AIToolDecisionControl detection={detection} />
      </div>
    ),
  },
];

/** One group of the Tool decisions list: every detected tool of one kind with
 *  its decision inline. Rows open the people the tool was detected for. */
export function AIToolDecisionTable({
  detections,
  isLoading,
  emptyState,
  onOpenTool,
}: {
  detections: AIDetection[];
  isLoading: boolean;
  emptyState: ReactNode;
  onOpenTool?: (detection: AIDetection) => void;
}): JSX.Element {
  if (isLoading) return <SkeletonTable />;
  if (detections.length === 0) return <>{emptyState}</>;
  return (
    <Table
      columns={COLUMNS}
      data={detections}
      rowKey={(detection) => detection.targetId}
      onRowClick={onOpenTool}
    />
  );
}
