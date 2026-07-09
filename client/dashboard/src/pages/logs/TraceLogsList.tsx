import { ReactElement } from "react";
import { cn } from "@/lib/utils";
import { telemetrySearchLogs } from "@gram/client/funcs/telemetrySearchLogs";
import type { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord.js";
import type { ToolUsageTraceLogGroup } from "@gram/client/models/components/toolusagetraceloggroup.js";
import { Operator as Op } from "@gram/client/models/components/logfilter";
import type { SearchLogsPayload } from "@gram/client/models/components/searchlogspayload";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { LoaderCircle } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { formatNanoTimestamp, formatLogBody } from "./utils";

function buildTraceFilters(
  logGroup: ToolUsageTraceLogGroup,
): Pick<SearchLogsPayload, "filter" | "filters"> | null {
  if (logGroup.kind === "correlation_id") {
    return {
      filters: [
        {
          path: "gram.trigger.correlation_id",
          operator: Op.Eq,
          values: [logGroup.value],
        },
      ],
    };
  }
  if (logGroup.kind === "trigger_event_id") {
    return {
      filters: [
        {
          path: "gram.trigger.event_id",
          operator: Op.Eq,
          values: [logGroup.value],
        },
      ],
    };
  }
  if (logGroup.kind === "trace_id") {
    return { filter: { traceId: logGroup.value } };
  }
  return null;
}

// Uses design system tokens where available (destructive, warning, muted).
// INFO has no semantic token — hardcoded Tailwind is intentional and matches
// the deployment logs palette (PR #2167).
const severityColors = {
  INFO: { dot: "bg-blue-500", text: "text-blue-700", bg: "bg-blue-50" },
  WARN: { dot: "bg-warning", text: "text-warning", bg: "bg-warning/10" },
  ERROR: {
    dot: "bg-destructive",
    text: "text-destructive",
    bg: "bg-destructive/10",
  },
  DEBUG: {
    dot: "bg-muted-foreground",
    text: "text-muted-foreground",
    bg: "bg-muted",
  },
} as const;

function getSeverityColors(severity?: string) {
  switch (severity?.toUpperCase()) {
    case "ERROR":
    case "FATAL":
      return severityColors.ERROR;
    case "WARN":
    case "WARNING":
      return severityColors.WARN;
    case "DEBUG":
      return severityColors.DEBUG;
    case undefined:
    default:
      return severityColors.INFO;
  }
}

interface TraceLogsListProps {
  logGroup: ToolUsageTraceLogGroup;
  toolName: string;
  isExpanded: boolean;
  onLogClick: (log: TelemetryLogRecord) => void;
  parentTimestamp: string;
  from: Date;
  to: Date;
}

export function TraceLogsList({
  logGroup,
  toolName: _toolName,
  isExpanded,
  onLogClick,
  parentTimestamp,
  from,
  to,
}: TraceLogsListProps): ReactElement | null {
  const client = useGramContext();
  const traceFilters = buildTraceFilters(logGroup);

  const { data, isPending, error } = useQuery({
    queryKey: [
      "trace-logs",
      logGroup.kind,
      logGroup.value,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () =>
      unwrapAsync(
        telemetrySearchLogs(client, {
          searchLogsPayload: {
            ...traceFilters,
            from,
            to,
            limit: 100,
            sort: "asc",
          },
        }),
      ),
    enabled: isExpanded && traceFilters !== null,
  });

  if (!isExpanded) {
    return null;
  }

  if (traceFilters === null) {
    return (
      <div className="text-muted-foreground bg-muted/30 flex items-center gap-3 px-5 py-2">
        <div className="w-1.5 shrink-0" />
        <div className="w-[150px] shrink-0" />
        <div className="w-5 shrink-0" />
        <span className="text-xs">No additional logs in this trace</span>
      </div>
    );
  }

  if (isPending) {
    return (
      <div className="text-muted-foreground bg-muted/30 flex items-center gap-3 px-5 py-2">
        <div className="w-1.5 shrink-0" />
        <div className="w-[150px] shrink-0" />
        <div className="flex w-5 shrink-0 justify-center">
          <LoaderCircle className="size-4 animate-spin" />
        </div>
        <span className="text-xs">Loading spans...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="bg-muted/30 flex items-center gap-3 px-5 py-2">
        <div className="w-1.5 shrink-0" />
        <div className="w-[150px] shrink-0" />
        <div className="w-5 shrink-0" />
        <span className="text-destructive text-xs">
          Failed to load spans: {error.message}
        </span>
      </div>
    );
  }

  const logs = data?.logs ?? [];

  if (logs.length === 0) {
    return (
      <div className="text-muted-foreground bg-muted/30 flex items-center gap-3 px-5 py-2">
        <div className="w-1.5 shrink-0" />
        <div className="w-[150px] shrink-0" />
        <div className="w-5 shrink-0" />
        <span className="text-xs">No spans found for this trace</span>
      </div>
    );
  }

  return (
    <div className="bg-muted/30">
      {logs.map((log, index) => (
        <ChildLogRow
          key={log.id}
          log={log}
          isLast={index === logs.length - 1}
          onClick={() => onLogClick(log)}
          parentTimestamp={parentTimestamp}
        />
      ))}
    </div>
  );
}

interface ChildLogRowProps {
  log: TelemetryLogRecord;
  isLast: boolean;
  onClick: () => void;
  parentTimestamp: string;
}

function ChildLogRow({
  log,
  isLast,
  onClick,
  parentTimestamp,
}: ChildLogRowProps) {
  const formattedTimestamp = formatNanoTimestamp(log.timeUnixNano);
  const formattedParentTimestamp = formatNanoTimestamp(parentTimestamp);
  const showTimestamp = formattedTimestamp !== formattedParentTimestamp;
  const colors = getSeverityColors(log.severityText);

  return (
    <div
      className="hover:bg-background group flex cursor-pointer items-center gap-3 px-5 py-1.5 transition-colors"
      onClick={onClick}
    >
      {/* Severity dot indicator for rapid left-edge scanning */}
      <span
        className={cn("size-1.5 shrink-0 rounded-full", colors.dot)}
        aria-hidden="true"
      />

      {/* Timestamp - same width as parent for alignment, hidden if same as parent */}
      <div className="text-muted-foreground/60 w-[150px] shrink-0 font-mono text-[11px] whitespace-nowrap tabular-nums">
        {showTimestamp ? formattedTimestamp : null}
      </div>

      {/* Tree line area - aligns with parent's chevron */}
      <div className="relative flex h-6 w-5 shrink-0 justify-center">
        {/* Vertical line */}
        <div
          className={`bg-border absolute left-1/2 w-px -translate-x-1/2 ${
            isLast ? "-top-2 h-5" : "-top-2 -bottom-2"
          }`}
        />
        {/* Horizontal connector */}
        <div className="bg-border absolute top-1/2 left-1/2 h-px w-3" />
      </div>

      {/* Severity badge inline */}
      <span
        className={cn(
          "shrink-0 rounded px-1.5 py-0.5 font-mono text-[10px] font-medium uppercase",
          colors.bg,
          colors.text,
        )}
      >
        {log.severityText?.toLowerCase() || "info"}
      </span>

      {/* Message - takes remaining space */}
      <span className="text-muted-foreground min-w-0 flex-1 truncate text-xs">
        {formatLogBody(log)}
      </span>
    </div>
  );
}
