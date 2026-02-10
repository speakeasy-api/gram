import { CopilotSidebar, useCopilotState } from "@/components/copilot-sidebar";
import { SearchBar } from "@/components/ui/search-bar";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { cn } from "@/lib/utils";
import { ServiceError } from "@gram/client/models/errors/serviceerror";
import { telemetrySearchToolCalls } from "@gram/client/funcs/telemetrySearchToolCalls";
import {
  FeatureName,
  TelemetryLogRecord,
  ToolCallSummary,
} from "@gram/client/models/components";
import {
  useFeaturesSetMutation,
  useGramContext,
} from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { useInfiniteQuery } from "@tanstack/react-query";
import { XIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { LogDetailSheet } from "./LogDetailSheet";
import { TraceRow } from "./TraceRow";

const perPage = 25;

export default function LogsPage() {
  // Copilot config - filter to logs-related tools only
  const logsToolFilter = useCallback(
    ({ toolName }: { toolName: string }) =>
      toolName.toLowerCase().includes("logs"),
    [],
  );
  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: logsToolFilter,
  });

  return (
    <CopilotSidebar
      mcpConfig={mcpConfig}
      title="Explore Logs"
      subtitle="Ask me about your logs! Powered by Elements + Gram MCP"
      suggestions={[
        {
          title: "Failing Tool Calls",
          label: "Summarize failing tool calls",
          prompt: "Summarize failing tool calls",
        },
        {
          title: "Visualize top tool calls",
          label: "Plot tool call counts",
          prompt: "Plot a chart of the top tool calls and their counts",
        },
        {
          title: "Recent Errors",
          label: "Find recent errors",
          prompt: "Search for recent error logs and summarize what's happening",
        },
      ]}
    >
      <LogsContent />
    </CopilotSidebar>
  );
}

function LogsContent() {
  const { isExpanded: isCopilotOpen } = useCopilotState();
  const [searchQuery, setSearchQuery] = useState<string | null>(null);
  const [searchInput, setSearchInput] = useState("");
  const [expandedTraceId, setExpandedTraceId] = useState<string | null>(null);
  const [selectedLog, setSelectedLog] = useState<TelemetryLogRecord | null>(
    null,
  );
  const containerRef = useRef<HTMLDivElement>(null);

  const client = useGramContext();

  const {
    data,
    error,
    fetchNextPage,
    hasNextPage,
    isFetching,
    isFetchingNextPage,
    refetch,
  } = useInfiniteQuery({
    queryKey: ["tool-calls", searchQuery],
    queryFn: ({ pageParam }) =>
      unwrapAsync(
        telemetrySearchToolCalls(client, {
          searchToolCallsPayload: {
            filter: searchQuery ? { gramUrn: searchQuery } : undefined,
            cursor: pageParam,
            limit: perPage,
            sort: "desc",
          },
        }),
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    throwOnError: false,
  });

  // Flatten all pages into a single array of traces
  const allTraces = data?.pages.flatMap((page) => page.toolCalls) ?? [];
  const logsEnabled = data?.pages[0]?.enabled ?? true;

  const [logsMutationError, setLogsMutationError] = useState<string | null>(
    null,
  );
  const { mutateAsync: setLogsFeature, status: logsMutationStatus } =
    useFeaturesSetMutation({
      onSuccess: () => {
        setLogsMutationError(null);
        refetch();
      },
      onError: (err) => {
        const message =
          err instanceof Error ? err.message : "Failed to update logs";
        setLogsMutationError(message);
      },
    });

  const isMutatingLogs = logsMutationStatus === "pending";

  const handleSetLogs = (enabled: boolean) => {
    setLogsMutationError(null);
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          featureName: FeatureName.Logs,
          enabled,
        },
      },
    });
  };

  // Debounce search input
  useEffect(() => {
    const timeoutId = setTimeout(() => {
      setSearchQuery(searchInput || null);
    }, 500);
    return () => clearTimeout(timeoutId);
  }, [searchInput]);

  // Handle scroll for infinite loading
  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const container = e.currentTarget;
    const scrollTop = container.scrollTop;
    const scrollHeight = container.scrollHeight;
    const clientHeight = container.clientHeight;
    const distanceFromBottom = scrollHeight - (scrollTop + clientHeight);

    if (isFetchingNextPage || isFetching) return;
    if (!hasNextPage) return;

    if (distanceFromBottom < 200) {
      fetchNextPage();
    }
  };

  const toggleExpand = (traceId: string) => {
    setExpandedTraceId((prev) => (prev === traceId ? null : traceId));
  };

  const handleLogClick = (log: TelemetryLogRecord) => {
    setSelectedLog(log);
  };

  const isLoading = isFetching && allTraces.length === 0;

  return (
    <div className="flex flex-col h-full w-full overflow-hidden">
      {/* Header section */}
      <div className="p-6 border-b shrink-0">
        <div
          className={cn(
            "flex gap-4 mb-4 transition-all duration-300",
            isCopilotOpen
              ? "flex-col items-stretch"
              : "flex-row items-center justify-between",
          )}
        >
          <div className="min-w-0">
            <h1 className="text-2xl font-semibold mb-1">Logs</h1>
            <p className="text-sm text-muted-foreground">
              Browse raw tool call traces and telemetry data
            </p>
          </div>
        </div>
        {/* Search Row */}
        <SearchBar
          value={searchInput}
          onChange={setSearchInput}
          placeholder="Search by tool URN"
          className="max-w-md"
        />
      </div>

      {/* Content section */}
      <div className="flex-1 overflow-hidden relative min-h-0">
        {/* Trace list container */}
        <div className="h-full flex flex-col bg-background">
          {/* Loading indicator */}
          {isFetching && allTraces.length > 0 && (
            <div className="absolute top-0 left-0 right-0 h-1 bg-primary/20 z-20">
              <div className="h-full bg-primary animate-pulse" />
            </div>
          )}

          {/* Header */}
          <div className="flex items-center gap-3 px-5 py-2.5 bg-muted/30 border-b text-xs font-medium text-muted-foreground uppercase tracking-wide shrink-0">
            <div className="shrink-0 w-[150px]">Timestamp</div>
            <div className="shrink-0 w-5" />
            <div className="flex-1">Source / Tool</div>
            <div className="shrink-0 w-16 text-right">Status</div>
          </div>

          {/* Scrollable trace list */}
          <div
            ref={containerRef}
            className="overflow-y-auto flex-1"
            onScroll={handleScroll}
          >
            <TraceListContent
              error={error}
              isLoading={isLoading}
              logsEnabled={logsEnabled}
              allTraces={allTraces}
              searchQuery={searchQuery}
              expandedTraceId={expandedTraceId}
              isFetchingNextPage={isFetchingNextPage}
              isMutatingLogs={isMutatingLogs}
              logsMutationError={logsMutationError}
              onEnableLogs={() => handleSetLogs(true)}
              onToggleExpand={toggleExpand}
              onLogClick={handleLogClick}
            />
          </div>

          {/* Footer */}
          {allTraces.length > 0 && (
            <div className="flex items-center justify-between gap-4 px-5 py-3 bg-muted/30 border-t text-sm text-muted-foreground shrink-0">
              <span>
                {allTraces.length} {allTraces.length === 1 ? "trace" : "traces"}
                {hasNextPage && " • Scroll to load more"}
              </span>
              {logsEnabled ? (
                <Button
                  onClick={() => handleSetLogs(false)}
                  disabled={isMutatingLogs}
                  size="sm"
                  variant="secondary"
                >
                  <Button.Text>
                    {isMutatingLogs ? "Updating..." : "Disable Logs"}
                  </Button.Text>
                </Button>
              ) : (
                <Button
                  onClick={() => handleSetLogs(true)}
                  disabled={isMutatingLogs}
                  size="sm"
                  variant="secondary"
                >
                  <Button.LeftIcon>
                    <Icon name="test-tube-diagonal" className="size-4" />
                  </Button.LeftIcon>
                  <Button.Text>
                    {isMutatingLogs ? "Updating..." : "Enable Logs"}
                  </Button.Text>
                </Button>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Log Detail Sheet */}
      <LogDetailSheet
        log={selectedLog}
        open={!!selectedLog}
        onOpenChange={(open) => !open && setSelectedLog(null)}
      />
    </div>
  );
}

function TraceListContent({
  error,
  isLoading,
  logsEnabled,
  allTraces,
  searchQuery,
  expandedTraceId,
  isFetchingNextPage,
  isMutatingLogs,
  logsMutationError,
  onEnableLogs,
  onToggleExpand,
  onLogClick,
}: {
  error: Error | null;
  isLoading: boolean;
  logsEnabled: boolean;
  allTraces: ToolCallSummary[];
  searchQuery: string | null;
  expandedTraceId: string | null;
  isFetchingNextPage: boolean;
  isMutatingLogs: boolean;
  logsMutationError: string | null;
  onEnableLogs: () => void;
  onToggleExpand: (traceId: string) => void;
  onLogClick: (log: TelemetryLogRecord) => void;
}) {
  if (error instanceof ServiceError && error.statusCode === 404) {
    return (
      <LogsDisabledState
        onEnableLogs={onEnableLogs}
        isMutating={isMutatingLogs}
        mutationError={logsMutationError}
      />
    );
  }

  if (error) {
    return (
      <LogsError
        error={
          error instanceof Error
            ? error
            : new Error("An unexpected error occurred")
        }
      />
    );
  }

  if (isLoading) {
    return <LogsLoading />;
  }

  if (allTraces.length === 0) {
    if (!logsEnabled) {
      return (
        <LogsDisabledState
          onEnableLogs={onEnableLogs}
          isMutating={isMutatingLogs}
          mutationError={logsMutationError}
        />
      );
    }
    return <LogsEmptyState searchQuery={searchQuery} />;
  }

  return (
    <>
      {allTraces.map((trace) => (
        <TraceRow
          key={trace.traceId}
          trace={trace}
          isExpanded={expandedTraceId === trace.traceId}
          onToggle={() => onToggleExpand(trace.traceId)}
          onLogClick={onLogClick}
        />
      ))}

      {isFetchingNextPage && (
        <div className="flex items-center justify-center gap-2 py-4 text-muted-foreground border-t">
          <Icon name="loader-circle" className="size-4 animate-spin" />
          <span className="text-sm">Loading more traces...</span>
        </div>
      )}
    </>
  );
}

function LogsDisabledState({
  onEnableLogs,
  isMutating,
  mutationError,
}: {
  onEnableLogs: () => void;
  isMutating: boolean;
  mutationError: string | null;
}) {
  return (
    <div className="py-12 text-center text-muted-foreground">
      <div className="flex flex-col items-center gap-3">
        <div className="size-12 rounded-full bg-muted flex items-center justify-center mb-2">
          <Icon name="scroll-text" className="size-6 text-muted-foreground" />
        </div>
        <span className="font-medium text-foreground">
          Logs are disabled for your organization
        </span>
        <span className="text-sm max-w-sm">
          Enable logs to capture tool call traces and telemetry data for
          debugging and analysis.
        </span>
        <Button
          onClick={onEnableLogs}
          disabled={isMutating}
          size="sm"
          className="mt-2"
        >
          <Button.LeftIcon>
            <Icon name="test-tube-diagonal" className="size-4" />
          </Button.LeftIcon>
          <Button.Text>
            {isMutating ? "Enabling..." : "Enable Logs"}
          </Button.Text>
        </Button>
        {mutationError && (
          <span className="text-sm text-destructive">{mutationError}</span>
        )}
      </div>
    </div>
  );
}

function LogsError({ error }: { error: Error }) {
  return (
    <div className="flex flex-col items-center gap-3 py-12">
      <div className="size-12 rounded-full bg-destructive/10 flex items-center justify-center">
        <XIcon className="size-6 text-destructive" />
      </div>
      <span className="font-medium text-foreground">Error loading traces</span>
      <span className="text-sm text-muted-foreground max-w-sm text-center">
        {error.message}
      </span>
    </div>
  );
}

function LogsLoading() {
  return (
    <div className="flex items-center justify-center gap-2 py-12 text-muted-foreground">
      <Icon name="loader-circle" className="size-5 animate-spin" />
      <span>Loading traces...</span>
    </div>
  );
}

function LogsEmptyState({ searchQuery }: { searchQuery: string | null }) {
  return (
    <div className="py-12 text-center">
      <div className="flex flex-col items-center gap-3">
        <div className="size-12 rounded-full bg-muted flex items-center justify-center">
          <Icon name="inbox" className="size-6 text-muted-foreground" />
        </div>
        <span className="font-medium text-foreground">
          {searchQuery ? "No matching traces" : "No traces found"}
        </span>
        <span className="text-sm text-muted-foreground max-w-sm">
          {searchQuery
            ? "Try adjusting your search query"
            : "Traces will appear here when tool calls are made"}
        </span>
      </div>
    </div>
  );
}
