import { telemetrySearchLogs } from "@gram/client/funcs/telemetrySearchLogs";
import { TelemetryLogRecord } from "@gram/client/models/components";
import { useGramContext } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Icon } from "@speakeasy-api/moonshine";
import { useInfiniteQuery } from "@tanstack/react-query";
import { formatNanoTimestamp } from "./utils";

function SeverityBadge({ severity }: { severity?: string }) {
  const upper = (severity ?? "INFO").toUpperCase();

  const styles: Record<string, string> = {
    ERROR:
      "bg-destructive-softest text-destructive-default",
    FATAL:
      "bg-destructive-softest text-destructive-default",
    WARN: "bg-warning-softest text-warning-default",
    WARNING: "bg-warning-softest text-warning-default",
    DEBUG:
      "bg-surface-secondary-default text-muted-foreground",
    INFO: "bg-primary-softest text-primary-default",
  };

  return (
    <span
      className={`px-1.5 py-0.5 text-[10px] font-medium rounded shrink-0 ${styles[upper] ?? styles.INFO}`}
    >
      {upper}
    </span>
  );
}

interface ConversationLogsListProps {
  chatId: string;
}

export function ConversationLogsList({ chatId }: ConversationLogsListProps) {
  const client = useGramContext();

  const { data, isFetching, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      queryKey: ["conversation-logs", chatId],
      queryFn: ({ pageParam }) =>
        unwrapAsync(
          telemetrySearchLogs(client, {
            searchLogsPayload: {
              filter: { gramChatId: chatId },
              cursor: pageParam,
              limit: 50,
              sort: "asc",
            },
          }),
        ),
      initialPageParam: undefined as string | undefined,
      getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    });

  const logs = data?.pages.flatMap((page) => page.logs) ?? [];
  const isLoading = isFetching && logs.length === 0;

  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const container = e.currentTarget;
    const distanceFromBottom =
      container.scrollHeight - (container.scrollTop + container.clientHeight);
    if (isFetchingNextPage || isFetching || !hasNextPage) return;
    if (distanceFromBottom < 200) {
      fetchNextPage();
    }
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center gap-2 py-8 text-muted-foreground">
        <Icon name="loader-circle" className="size-4 animate-spin" />
        <span className="text-sm">Loading logs...</span>
      </div>
    );
  }

  if (logs.length === 0) {
    return (
      <div className="py-8 text-center text-sm text-muted-foreground">
        No logs found for this conversation
      </div>
    );
  }

  return (
    <div className="flex flex-col overflow-y-auto" onScroll={handleScroll}>
      {logs.map((log) => (
        <LogEntry key={log.id} log={log} />
      ))}
      {isFetchingNextPage && (
        <div className="flex items-center justify-center gap-2 py-3 text-muted-foreground">
          <Icon name="loader-circle" className="size-3 animate-spin" />
          <span className="text-xs">Loading more...</span>
        </div>
      )}
    </div>
  );
}

function LogEntry({ log }: { log: TelemetryLogRecord }) {
  return (
    <div className="flex gap-3 px-4 py-2.5 border-b border-border/30 last:border-b-0 hover:bg-surface-secondary-default/50 transition-colors">
      <div className="flex flex-col items-end gap-1 shrink-0 w-[100px]">
        <span className="text-[10px] font-mono text-muted-foreground">
          {formatNanoTimestamp(log.timeUnixNano)}
        </span>
        <SeverityBadge severity={log.severityText} />
      </div>
      <div className="flex-1 min-w-0">
        <p className="text-sm break-words">{log.body || "(no message)"}</p>
        {log.service && (
          <span className="text-[10px] text-muted-foreground">
            {log.service.name}
            {log.service.version ? ` v${log.service.version}` : ""}
          </span>
        )}
      </div>
    </div>
  );
}
