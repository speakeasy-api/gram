import { telemetrySearchLogs } from "@gram/client/funcs/telemetrySearchLogs";
import { TelemetryLogRecord } from "@gram/client/models/components";
import { useGramContext } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Icon } from "@speakeasy-api/moonshine";
import { useQuery } from "@tanstack/react-query";
import { formatNanoTimestamp, formatLogBody } from "./utils";

interface TraceLogsListProps {
  traceId: string;
  toolName: string;
  isExpanded: boolean;
  onLogClick: (log: TelemetryLogRecord) => void;
  parentTimestamp: number;
}

export function TraceLogsList({
  traceId,
  toolName: _toolName,
  isExpanded,
  onLogClick,
  parentTimestamp,
}: TraceLogsListProps) {
  const client = useGramContext();

  const { data, isPending, error } = useQuery({
    queryKey: ["trace-logs", traceId],
    queryFn: () =>
      unwrapAsync(
        telemetrySearchLogs(client, {
          searchLogsPayload: {
            filter: {
              traceId,
            },
            limit: 100,
            sort: "asc",
          },
        }),
      ),
    enabled: isExpanded,
  });

  if (!isExpanded) {
    return null;
  }

  if (isPending) {
    return (
      <div className="flex items-center gap-3 px-5 py-2 text-muted-foreground bg-muted/30">
        <div className="shrink-0 w-[150px]" />
        <div className="shrink-0 w-5 flex justify-center">
          <Icon name="loader-circle" className="size-4 animate-spin" />
        </div>
        <span className="text-sm">Loading spans...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center gap-3 px-5 py-2 bg-muted/30">
        <div className="shrink-0 w-[150px]" />
        <div className="shrink-0 w-5" />
        <span className="text-sm text-destructive">
          Failed to load spans: {error.message}
        </span>
      </div>
    );
  }

  const logs = data?.logs ?? [];

  if (logs.length === 0) {
    return (
      <div className="flex items-center gap-3 px-5 py-2 text-muted-foreground bg-muted/30">
        <div className="shrink-0 w-[150px]" />
        <div className="shrink-0 w-5" />
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
  parentTimestamp: number;
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

  return (
    <div
      className="flex items-center gap-3 px-5 py-2 cursor-pointer hover:bg-background transition-colors group"
      onClick={onClick}
    >
      {/* Timestamp - same width as parent for alignment, hidden if same as parent */}
      <div className="shrink-0 w-[150px] text-sm text-muted-foreground font-mono whitespace-nowrap">
        {showTimestamp ? formattedTimestamp : null}
      </div>

      {/* Tree line area - aligns with parent's chevron */}
      <div className="shrink-0 w-5 flex justify-center relative h-6">
        {/* Vertical line */}
        <div
          className={`absolute left-1/2 -translate-x-1/2 w-px bg-border ${
            isLast ? "-top-2 h-5" : "-top-2 -bottom-2"
          }`}
        />
        {/* Horizontal connector */}
        <div className="absolute top-1/2 left-1/2 w-3 h-px bg-border" />
      </div>

      {/* Severity badge inline */}
      <span
        className={`shrink-0 px-1.5 py-0.5 text-xs font-medium rounded ${getSeverityBadgeClass(log.severityText)}`}
      >
        {log.severityText?.toLowerCase() || "info"}
      </span>

      {/* Message - takes remaining space */}
      <span className="flex-1 min-w-0 text-sm text-muted-foreground truncate">
        {formatLogBody(log)}
      </span>
    </div>
  );
}

function getSeverityBadgeClass(severity?: string): string {
  switch (severity?.toUpperCase()) {
    case "ERROR":
    case "FATAL":
      return "bg-rose-500/15 text-rose-600 dark:text-rose-400";
    case "WARN":
    case "WARNING":
      return "bg-amber-500/15 text-amber-600 dark:text-amber-400";
    case "DEBUG":
      return "bg-muted text-muted-foreground";
    case "INFO":
    default:
      return "bg-blue-500/15 text-blue-600 dark:text-blue-400";
  }
}
