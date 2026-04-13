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
      <div className="text-muted-foreground bg-muted/30 flex items-center gap-3 px-5 py-2">
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

  return (
    <div
      className="hover:bg-background group flex cursor-pointer items-center gap-3 px-5 py-2 transition-colors"
      onClick={onClick}
    >
      {/* Timestamp - same width as parent for alignment, hidden if same as parent */}
      <div className="text-muted-foreground w-[150px] shrink-0 font-mono text-sm whitespace-nowrap">
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
        className={`shrink-0 rounded px-1.5 py-0.5 text-xs font-medium ${getSeverityBadgeClass(log.severityText)}`}
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
