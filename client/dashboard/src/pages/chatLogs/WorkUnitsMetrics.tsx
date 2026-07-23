import { Badge, Icon, type IconName } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { Dialog } from "@/components/ui/dialog";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { formatTokenCount, formatUsageCost } from "./claudeUsage";
import {
  type WorkUnitsChatFields,
  type WorkUnitsTask,
  formatWorkUnits,
  parseWorkUnitsReport,
  workUnitsEfficiency,
} from "./workUnits";

/** "Work done" plus efficiency entries for an agent-sessions table row.
 * Renders nothing when the session has no work-units score, which is the
 * norm: most organizations don't run the analysis. */
function WorkMetric({
  icon,
  tooltip,
  value,
}: {
  icon: IconName;
  tooltip: string;
  value: string;
}): JSX.Element {
  return (
    <SimpleTooltip tooltip={tooltip}>
      <span className="flex items-center gap-1 tabular-nums">
        <Icon name={icon} className="size-4 opacity-60" />
        {value}
      </span>
    </SimpleTooltip>
  );
}

export function WorkUnitsRowMetrics({
  chat,
}: {
  chat: WorkUnitsChatFields;
}): JSX.Element | null {
  if (chat.workUnits === undefined) return null;
  const { costPerUnit, tokensPerUnit } = workUnitsEfficiency(chat);

  return (
    <span
      className="border-border/60 inline-flex items-center gap-2 border-l pl-2"
      aria-label="Work analysis metrics"
    >
      <WorkMetric
        icon="hammer"
        tooltip="Work delivered in this session, as judged by work analysis"
        value={formatWorkUnits(chat.workUnits)}
      />
      {costPerUnit !== null && (
        <WorkMetric
          icon="circle-dollar-sign"
          tooltip="Cost efficiency — spend relative to work delivered"
          value={formatUsageCost(costPerUnit)}
        />
      )}
      {tokensPerUnit !== null && (
        <WorkMetric
          icon="binary"
          tooltip="Token efficiency — tokens relative to work delivered"
          value={formatTokenCount(tokensPerUnit)}
        />
      )}
    </span>
  );
}

function WorkUnitsMetricBadge({ children }: { children: React.ReactNode }) {
  return (
    <Badge variant="neutral" className="shrink-0 font-mono text-[10px]">
      <Badge.Text>{children}</Badge.Text>
    </Badge>
  );
}

function WorkUnitsFlagBadges({ flags }: { flags: string[] }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      {flags.map((flag) => (
        <Badge key={flag} variant="warning" className="text-[10px]">
          <Badge.Text>{flag.replaceAll("_", " ")}</Badge.Text>
        </Badge>
      ))}
    </div>
  );
}

function WorkUnitsTaskRow({
  task,
  index,
}: {
  task: WorkUnitsTask;
  index: number;
}) {
  return (
    <div className="border-border/50 border-t py-3 first:border-t-0 first:pt-0">
      <div className="flex items-start justify-between gap-3">
        <p className="text-sm leading-snug font-medium">
          {task.request || `Task ${task.id ?? index + 1}`}
        </p>
        <span className="shrink-0 text-sm font-semibold tabular-nums">
          Work delivered: {formatWorkUnits(task.units ?? 0)}
        </span>
      </div>
      <div className="text-muted-foreground mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs">
        {task.band && <span>Band {task.band}</span>}
        {task.completion !== undefined && (
          <span>{Math.round(task.completion * 100)}% complete</span>
        )}
        {task.nearest_exemplar && <span>≈ {task.nearest_exemplar}</span>}
      </div>
      {task.rationale && (
        <p className="text-muted-foreground mt-1.5 text-xs leading-relaxed">
          {task.rationale}
        </p>
      )}
    </div>
  );
}

/** Clickable work-units metric badges for the chat details drawer header.
 * Clicking opens the complete analysis report. Renders nothing when the
 * session has no work-units score. */
export function WorkUnitsHeaderMetrics({
  chat,
}: {
  chat: WorkUnitsChatFields;
}): JSX.Element | null {
  const [open, setOpen] = useState(false);
  if (chat.workUnits === undefined) return null;

  const { costPerUnit, tokensPerUnit } = workUnitsEfficiency(chat);
  const report = chat.workUnitsReport
    ? parseWorkUnitsReport(chat.workUnitsReport)
    : null;

  const badges = (
    <>
      <WorkUnitsMetricBadge>
        {formatWorkUnits(chat.workUnits)} work delivered
      </WorkUnitsMetricBadge>
      {costPerUnit !== null && (
        <WorkUnitsMetricBadge>
          Cost efficiency: {formatUsageCost(costPerUnit)}
        </WorkUnitsMetricBadge>
      )}
      {tokensPerUnit !== null && (
        <WorkUnitsMetricBadge>
          Token efficiency: {formatTokenCount(tokensPerUnit)}
        </WorkUnitsMetricBadge>
      )}
    </>
  );

  if (!chat.workUnitsReport) {
    return <div className="flex flex-wrap items-center gap-2">{badges}</div>;
  }

  return (
    <>
      <SimpleTooltip tooltip="View the full work analysis">
        <button
          type="button"
          onClick={() => setOpen(true)}
          className={cn(
            "flex flex-wrap items-center gap-2 rounded-md transition-opacity",
            "cursor-pointer hover:opacity-80",
            "focus-visible:ring-ring focus-visible:ring-2 focus:outline-none",
          )}
        >
          {badges}
        </button>
      </SimpleTooltip>
      <Dialog open={open} onOpenChange={setOpen}>
        <Dialog.Content className="max-w-2xl">
          <Dialog.Header>
            <Dialog.Title>Work analysis</Dialog.Title>
            <Dialog.Description>
              How the session analysis judged the work delivered in this
              session, task by task.
            </Dialog.Description>
          </Dialog.Header>
          <div className="flex flex-wrap items-center gap-2">{badges}</div>
          {report?.flags && report.flags.length > 0 && (
            <WorkUnitsFlagBadges flags={report.flags} />
          )}
          <div className="max-h-[55vh] overflow-y-auto pr-1">
            {report?.tasks && report.tasks.length > 0 ? (
              report.tasks.map((task, index) => (
                <WorkUnitsTaskRow
                  key={task.id ?? index}
                  task={task}
                  index={index}
                />
              ))
            ) : (
              <pre className="text-muted-foreground text-xs leading-relaxed whitespace-pre-wrap">
                {chat.workUnitsReport}
              </pre>
            )}
          </div>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
