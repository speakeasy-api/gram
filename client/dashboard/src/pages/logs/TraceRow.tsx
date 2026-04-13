import {
  TelemetryLogRecord,
  ToolCallSummary,
} from "@gram/client/models/components";
import { ChevronDownIcon, ChevronRightIcon } from "lucide-react";
import { StatusBadge } from "./StatusBadge";
import { TraceLogsList } from "./TraceLogsList";
import {
  formatNanoTimestamp,
  getSourceFromUrn,
  getStatusInfo,
  getToolIcon,
  getToolNameFromUrn,
} from "./utils";

interface TraceRowProps {
  trace: ToolCallSummary;
  isExpanded: boolean;
  onToggle: () => void;
  onLogClick: (log: TelemetryLogRecord) => void;
}

export function TraceRow({
  trace,
  isExpanded,
  onToggle,
  onLogClick,
}: TraceRowProps) {
  const { isSuccess } = getStatusInfo(trace);
  const sourceName = trace.toolSource || getSourceFromUrn(trace.gramUrn);
  const toolName = trace.toolName || getToolNameFromUrn(trace.gramUrn);
  const ToolIcon = getToolIcon(trace);

  return (
    <div className="border-border/50 border-b last:border-b-0">
      {/* Parent trace row */}
      <div
        className="hover:bg-muted/50 flex cursor-pointer items-center gap-3 px-8 py-2.5 transition-colors"
        onClick={onToggle}
      >
        {/* Timestamp */}
        <div className="text-muted-foreground w-[150px] shrink-0 font-mono text-sm whitespace-nowrap">
          {formatNanoTimestamp(trace.startTimeUnixNano)}
        </div>

        {/* Expand/collapse indicator */}
        <div className="flex w-5 shrink-0 items-center justify-center">
          {isExpanded ? (
            <ChevronDownIcon className="text-muted-foreground size-4" />
          ) : (
            <ChevronRightIcon className="text-muted-foreground size-4" />
          )}
        </div>

        {/* Icon + Source badge + Tool name */}
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <ToolIcon className="size-4 shrink-0" strokeWidth={1.5} />
          {sourceName && (
            <span className="bg-muted text-muted-foreground shrink-0 rounded px-1.5 py-0.5 text-xs font-medium">
              {sourceName}
            </span>
          )}
          <span className="truncate font-mono text-sm">{toolName}</span>
        </div>

        {/* Log count */}
        <span className="text-muted-foreground shrink-0 text-xs tabular-nums">
          {trace.logCount} {trace.logCount === 1 ? "log" : "logs"}
        </span>

        {/* Status badge */}
        <StatusBadge
          isSuccess={isSuccess}
          httpStatusCode={trace.httpStatusCode}
        />
      </div>

      {/* Expanded child logs */}
      {isExpanded && (
        <TraceLogsList
          traceId={trace.traceId}
          toolName={toolName}
          isExpanded={isExpanded}
          onLogClick={onLogClick}
          parentTimestamp={trace.startTimeUnixNano}
        />
      )}
    </div>
  );
}
