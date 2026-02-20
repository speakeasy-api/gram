import {
  ToolCallSummary,
  TelemetryLogRecord,
} from "@gram/client/models/components";
import { ChevronDownIcon, ChevronRightIcon } from "lucide-react";
import {
  formatNanoTimestamp,
  getStatusInfo,
  getSourceFromUrn,
  getToolNameFromUrn,
  getToolIcon,
} from "./utils";
import { TraceLogsList } from "./TraceLogsList";
import { StatusBadge } from "./StatusBadge";

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
  const sourceName = getSourceFromUrn(trace.gramUrn);
  const toolName = getToolNameFromUrn(trace.gramUrn);
  const ToolIcon = getToolIcon(trace.gramUrn);

  return (
    <div className="border-b border-border/50 last:border-b-0">
      {/* Parent trace row */}
      <div
        className="flex items-center gap-3 px-5 py-2.5 cursor-pointer hover:bg-muted/50 transition-colors"
        onClick={onToggle}
      >
        {/* Timestamp */}
        <div className="shrink-0 w-[150px] text-sm text-muted-foreground font-mono whitespace-nowrap">
          {formatNanoTimestamp(trace.startTimeUnixNano)}
        </div>

        {/* Expand/collapse indicator */}
        <div className="shrink-0 w-5 flex items-center justify-center">
          {isExpanded ? (
            <ChevronDownIcon className="size-4 text-muted-foreground" />
          ) : (
            <ChevronRightIcon className="size-4 text-muted-foreground" />
          )}
        </div>

        {/* Icon + Source badge + Tool name */}
        <div className="flex items-center gap-2 flex-1 min-w-0">
          <ToolIcon className="size-4 shrink-0" strokeWidth={1.5} />
          <span className="shrink-0 px-1.5 py-0.5 text-xs font-medium rounded bg-muted text-muted-foreground">
            {sourceName}
          </span>
          <span className="text-sm font-mono truncate">{toolName}</span>
        </div>

        {/* Log count */}
        <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
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
