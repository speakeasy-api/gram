import { cn } from "@/lib/utils";
import { telemetrySearchLogs } from "@gram/client/funcs/telemetrySearchLogs";
import { TelemetryLogRecord } from "@gram/client/models/components";
import { Operator as Op } from "@gram/client/models/components/logfilter";
import type { SearchLogsPayload } from "@gram/client/models/components/searchlogspayload";
import { useGramContext } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Icon } from "@speakeasy-api/moonshine";
import { useQuery } from "@tanstack/react-query";
import { formatNanoTimestamp, formatLogBody } from "./utils";

// Standalone trigger logs (no correlation_id, no event_id) are grouped under
// `trigger-log:<log.id>` in use-attribute-logs-query.ts. The originating log
// is already rendered as the group header, so there are no sub-spans to fetch.
const TRIGGER_LOG_PREFIX = "trigger-log:";

function buildTraceFilters(
  traceId: string,
): Pick<SearchLogsPayload, "filter" | "filters"> {
  if (traceId.startsWith("corr:")) {
    return {
      filters: [
        {
          path: "gram.trigger.correlation_id",
          operator: Op.Eq,
          values: [traceId.slice("corr:".length)],
        },
      ],
    };
  }
  if (traceId.startsWith("trigger:")) {
    return {
      filters: [
        {
          path: "gram.trigger.event_id",
          operator: Op.Eq,
          values: [traceId.slice("trigger:".length)],
        },
      ],
    };
  }
  return { filter: { traceId } };
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
    default:
      return severityColors.INFO;
  }
}

interface TraceLogsListProps {
  traceId: string;
  toolName: string;
  isExpanded: boolean;
  onLogClick: (log: TelemetryLogRecord) => void;
  parentTimestamp: string;
}

export function TraceLogsList({
  traceId,
  toolName: _toolName,
  isExpanded,
  onLogClick,
  parentTimestamp,
}: TraceLogsListProps) {
  const client = useGramContext();
  const isStandaloneTriggerLog = traceId.startsWith(TRIGGER_LOG_PREFIX);

  const { data, isPending, error } = useQuery({
    queryKey: ["trace-logs", traceId],
    queryFn: () =>
      unwrapAsync(
        telemetrySearchLogs(client, {
          searchLogsPayload: {
            ...buildTraceFilters(traceId),
            limit: 100,
            sort: "asc",
          },
        }),
      ),
    enabled: isExpanded && !isStandaloneTriggerLog,
  });

  if (!isExpanded) {
    return null;
  }

  if (isStandaloneTriggerLog) {
    return (
      <div className="text-muted-foreground bg-muted/30 flex items-center gap-3 px-5 py-2">
        <div className="w-1.5 shrink-0" />
        <div className="w-[150px] shrink-0" />
        <div className="w-5 shrink-0" />
        <span className="text-sm">No additional logs in this trace</span>
      </div>
    );
  }

  if (isPending) {
    return (
      <div className="text-muted-foreground bg-muted/30 flex items-center gap-3 px-5 py-2">
        <div className="w-1.5 shrink-0" />
        <div className="w-[150px] shrink-0" />
        <div className="flex w-5 shrink-0 justify-center">
          <Icon name="loader-circle" className="size-4 animate-spin" />
        </div>
        <span className="text-sm">Loading spans...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="bg-muted/30 flex items-center gap-3 px-5 py-2">
        <div className="w-1.5 shrink-0" />
        <div className="w-[150px] shrink-0" />
        <div className="w-5 shrink-0" />
        <span className="text-destructive text-sm">
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
        <span className="text-sm">No spans found for this trace</span>
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
      <span className="text-muted-foreground min-w-0 flex-1 truncate text-sm">
        {formatLogBody(log)}
      </span>
    </div>
  );
}
