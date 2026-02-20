import {
  InsightsSidebar,
  useInsightsState,
} from "@/components/insights-sidebar";
import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { Page } from "@/components/page-layout";
import { SearchBar } from "@/components/ui/search-bar";
import { Switch } from "@/components/ui/switch";
import { SimpleTooltip } from "@/components/ui/tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@speakeasy-api/moonshine";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { cn } from "@/lib/utils";
import { useSlugs } from "@/contexts/Sdk";
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
import {
  TimeRangePicker,
  type DateRangePreset,
  getPresetRange,
} from "@gram-ai/elements";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { useInfiniteQuery } from "@tanstack/react-query";
import { MoreHorizontal, XIcon } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { LogDetailSheet } from "./LogDetailSheet";
import { TraceRow } from "./TraceRow";

// Valid date range presets
const validPresets: DateRangePreset[] = [
  "15m",
  "1h",
  "4h",
  "1d",
  "2d",
  "3d",
  "7d",
  "15d",
  "30d",
  "90d",
];

function isValidPreset(value: string | null): value is DateRangePreset {
  return value !== null && validPresets.includes(value as DateRangePreset);
}

// Safely encode a string to base64, handling non-Latin1 characters
function safeBase64Encode(str: string): string {
  try {
    return btoa(str);
  } catch {
    // btoa fails on non-Latin1 chars, so encode to URI component first
    return btoa(encodeURIComponent(str));
  }
}

// Safely decode a base64 string, handling URI-encoded content
function safeBase64Decode(str: string): string | null {
  try {
    const decoded = atob(str);
    // Check if it was URI-encoded by trying to decode
    try {
      return decodeURIComponent(decoded);
    } catch {
      return decoded;
    }
  } catch {
    return null;
  }
}

const perPage = 25;

export default function LogsPage() {
  return <LogsContent />;
}

function LogsContent() {
  const [searchParams, setSearchParams] = useSearchParams();
  const { projectSlug } = useSlugs();

  // Copilot config - filter to logs-related tools only
  const logsToolFilter = useCallback(
    ({ toolName }: { toolName: string }) =>
      toolName.toLowerCase().includes("logs"),
    [],
  );
  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: logsToolFilter,
  });
  const [searchQuery, setSearchQuery] = useState<string | null>(null);
  const [searchInput, setSearchInput] = useState("");
  const [expandedTraceId, setExpandedTraceId] = useState<string | null>(null);
  const [selectedLog, setSelectedLog] = useState<TelemetryLogRecord | null>(
    null,
  );
  const containerRef = useRef<HTMLDivElement>(null);

  const client = useGramContext();

  // Parse URL params for time range
  const urlRange = searchParams.get("range");
  const urlFrom = searchParams.get("from");
  const urlTo = searchParams.get("to");
  const urlLabelEncoded = searchParams.get("label");
  const urlLabel = useMemo(() => {
    if (!urlLabelEncoded) return null;
    return safeBase64Decode(urlLabelEncoded);
  }, [urlLabelEncoded]);

  // Derive state from URL
  const dateRange: DateRangePreset = isValidPreset(urlRange) ? urlRange : "7d";

  const customRange = useMemo(() => {
    if (urlFrom && urlTo) {
      const from = new Date(urlFrom);
      const to = new Date(urlTo);
      if (!isNaN(from.getTime()) && !isNaN(to.getTime())) {
        return { from, to };
      }
    }
    return null;
  }, [urlFrom, urlTo]);

  // Update URL helpers
  const updateSearchParams = useCallback(
    (updates: Record<string, string | null>) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        for (const [key, value] of Object.entries(updates)) {
          if (value === null) {
            next.delete(key);
          } else {
            next.set(key, value);
          }
        }
        return next;
      });
    },
    [setSearchParams],
  );

  const setDateRangeParam = useCallback(
    (preset: DateRangePreset) => {
      updateSearchParams({
        range: preset,
        from: null,
        to: null,
        label: null,
      });
    },
    [updateSearchParams],
  );

  const setCustomRangeParam = useCallback(
    (from: Date, to: Date, label?: string) => {
      updateSearchParams({
        range: null,
        from: from.toISOString(),
        to: to.toISOString(),
        label: label ? safeBase64Encode(label) : null,
      });
    },
    [updateSearchParams],
  );

  const clearCustomRange = useCallback(() => {
    updateSearchParams({
      from: null,
      to: null,
      label: null,
    });
  }, [updateSearchParams]);

  // Use custom range if set, otherwise use preset
  const { from, to } = useMemo(
    () => customRange ?? getPresetRange(dateRange),
    [customRange, dateRange],
  );

  const {
    data,
    error,
    fetchNextPage,
    hasNextPage,
    isFetching,
    isFetchingNextPage,
    refetch,
    isLogsDisabled,
  } = useLogsEnabledErrorCheck(
    useInfiniteQuery({
      queryKey: [
        "tool-calls",
        searchQuery,
        from.toISOString(),
        to.toISOString(),
      ],
      queryFn: ({ pageParam }) =>
        unwrapAsync(
          telemetrySearchToolCalls(client, {
            searchToolCallsPayload: {
              filter: {
                gramUrn: searchQuery || undefined,
                from,
                to,
              },
              cursor: pageParam,
              limit: perPage,
              sort: "desc",
            },
          }),
        ),
      initialPageParam: undefined as string | undefined,
      getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
      throwOnError: false,
    }),
  );

  // Flatten all pages into a single array of traces
  const allTraces = data?.pages.flatMap((page) => page.toolCalls) ?? [];
  const toolIoLogsEnabled = data?.pages[0]?.toolIoLogsEnabled ?? false;

  const { mutate: setLogsFeature, status: logsMutationStatus } =
    useFeaturesSetMutation({
      onSuccess: () => {
        refetch();
      },
    });

  const isMutatingLogs = logsMutationStatus === "pending";

  const handleSetLogs = (enabled: boolean) => {
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          featureName: FeatureName.Logs,
          enabled,
        },
      },
    });
  };

  const handleSetToolIoLogs = (enabled: boolean) => {
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          featureName: FeatureName.ToolIoLogs,
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
    <InsightsSidebar
      mcpConfig={mcpConfig}
      title="Explore Logs"
      subtitle="Ask me about your logs! Powered by Elements + Gram MCP"
      hideTrigger={isLogsDisabled}
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
      <LogsInnerContent
        isLogsDisabled={isLogsDisabled}
        isLoading={isLoading}
        isFetching={isFetching}
        error={error}
        allTraces={allTraces}
        searchQuery={searchQuery}
        searchInput={searchInput}
        setSearchInput={setSearchInput}
        expandedTraceId={expandedTraceId}
        toggleExpand={toggleExpand}
        selectedLog={selectedLog}
        handleLogClick={handleLogClick}
        setSelectedLog={setSelectedLog}
        containerRef={containerRef}
        handleScroll={handleScroll}
        hasNextPage={hasNextPage}
        isFetchingNextPage={isFetchingNextPage}
        isMutatingLogs={isMutatingLogs}
        handleSetLogs={handleSetLogs}
        toolIoLogsEnabled={toolIoLogsEnabled}
        handleSetToolIoLogs={handleSetToolIoLogs}
        refetch={refetch}
        // Time range props
        dateRange={dateRange}
        customRange={customRange}
        customRangeLabel={urlLabel}
        onDateRangeChange={setDateRangeParam}
        onCustomRangeChange={setCustomRangeParam}
        onClearCustomRange={clearCustomRange}
        projectSlug={projectSlug}
      />
    </InsightsSidebar>
  );
}

function LogsInnerContent({
  isLogsDisabled,
  isLoading,
  isFetching,
  error,
  allTraces,
  searchQuery,
  searchInput,
  setSearchInput,
  expandedTraceId,
  toggleExpand,
  selectedLog,
  handleLogClick,
  setSelectedLog,
  containerRef,
  handleScroll,
  hasNextPage,
  isFetchingNextPage,
  isMutatingLogs,
  handleSetLogs,
  toolIoLogsEnabled,
  handleSetToolIoLogs,
  refetch,
  // Time range props
  dateRange,
  customRange,
  customRangeLabel,
  onDateRangeChange,
  onCustomRangeChange,
  onClearCustomRange,
  projectSlug,
}: {
  isLogsDisabled: boolean;
  isLoading: boolean;
  isFetching: boolean;
  error: Error | null;
  allTraces: ToolCallSummary[];
  searchQuery: string | null;
  searchInput: string;
  setSearchInput: (value: string) => void;
  expandedTraceId: string | null;
  toggleExpand: (traceId: string) => void;
  selectedLog: TelemetryLogRecord | null;
  handleLogClick: (log: TelemetryLogRecord) => void;
  setSelectedLog: (log: TelemetryLogRecord | null) => void;
  containerRef: React.RefObject<HTMLDivElement | null>;
  handleScroll: (e: React.UIEvent<HTMLDivElement>) => void;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  isMutatingLogs: boolean;
  handleSetLogs: (enabled: boolean) => void;
  toolIoLogsEnabled: boolean;
  handleSetToolIoLogs: (enabled: boolean) => void;
  refetch: () => void;
  // Time range props
  dateRange: DateRangePreset;
  customRange: { from: Date; to: Date } | null;
  customRangeLabel: string | null;
  onDateRangeChange: (preset: DateRangePreset) => void;
  onCustomRangeChange: (from: Date, to: Date, label?: string) => void;
  onClearCustomRange: () => void;
  projectSlug?: string;
}) {
  const { isExpanded: isInsightsOpen } = useInsightsState();

  if (isLogsDisabled) {
    return (
      <div className="h-full overflow-hidden flex flex-col">
        <Page>
          <Page.Header>
            <Page.Header.Breadcrumbs fullWidth />
          </Page.Header>
          <Page.Body fullWidth className="space-y-6">
            <div className="flex flex-col gap-1 min-w-0">
              <h1 className="text-xl font-semibold">Logs</h1>
              <p className="text-sm text-muted-foreground">
                Browse raw tool call traces and telemetry data
              </p>
            </div>
            <div className="flex-1 relative">
              <div
                className="pointer-events-none select-none h-full"
                aria-hidden="true"
              >
                <ObservabilitySkeleton />
              </div>
              <EnableLoggingOverlay onEnabled={refetch} />
            </div>
          </Page.Body>
        </Page>
      </div>
    );
  }

  return (
    <>
      <div className="h-full overflow-hidden flex flex-col">
        <Page>
          <Page.Header>
            <Page.Header.Breadcrumbs fullWidth />
          </Page.Header>
          <Page.Body fullWidth noPadding overflowHidden>
            <div className="flex flex-col flex-1 min-h-0 w-full">
              {/* Header section */}
              <div className="px-8 py-4 shrink-0">
                <div
                  className={cn(
                    "flex gap-4 mb-4 transition-all duration-300",
                    isInsightsOpen
                      ? "flex-col items-stretch"
                      : "flex-row items-center justify-between",
                  )}
                >
                  <div className="flex flex-col gap-1 min-w-0">
                    <h1 className="text-xl font-semibold">Logs</h1>
                    <p className="text-sm text-muted-foreground">
                      Browse raw tool call traces and telemetry data
                    </p>
                  </div>
                  <div
                    className={cn(
                      "flex items-center gap-3",
                      isInsightsOpen ? "justify-start" : "flex-shrink-0",
                    )}
                  >
                    <TimeRangePicker
                      preset={customRange ? null : dateRange}
                      customRange={customRange}
                      customRangeLabel={customRangeLabel}
                      onPresetChange={onDateRangeChange}
                      onCustomRangeChange={onCustomRangeChange}
                      onClearCustomRange={onClearCustomRange}
                      projectSlug={projectSlug}
                    />
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="tertiary" size="sm">
                          <MoreHorizontal className="size-4" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <SimpleTooltip tooltip="Enabling this may expose sensitive data in logs.">
                          <DropdownMenuItem
                            onClick={(e) => {
                              e.preventDefault();
                              handleSetToolIoLogs(!toolIoLogsEnabled);
                            }}
                            disabled={isMutatingLogs}
                          >
                            <div className="flex items-center justify-between w-full gap-3">
                              <span>Record tool I/O</span>
                              <Switch
                                checked={toolIoLogsEnabled}
                                onCheckedChange={handleSetToolIoLogs}
                                disabled={isMutatingLogs}
                                aria-label="Record tool inputs & outputs"
                              />
                            </div>
                          </DropdownMenuItem>
                        </SimpleTooltip>
                        <DropdownMenuItem
                          onClick={() => handleSetLogs(false)}
                          disabled={isMutatingLogs}
                          className="text-destructive focus:text-destructive"
                        >
                          {isMutatingLogs ? "Updating..." : "Disable Logs"}
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
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

              {/* Content section - full width */}
              <div className="flex-1 overflow-hidden min-h-0 border-t">
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
                      allTraces={allTraces}
                      searchQuery={searchQuery}
                      expandedTraceId={expandedTraceId}
                      isFetchingNextPage={isFetchingNextPage}
                      onToggleExpand={toggleExpand}
                      onLogClick={handleLogClick}
                    />
                  </div>

                  {/* Footer */}
                  {allTraces.length > 0 && (
                    <div className="flex items-center gap-4 px-5 py-3 bg-muted/30 border-t text-sm text-muted-foreground shrink-0">
                      <span>
                        {allTraces.length}{" "}
                        {allTraces.length === 1 ? "trace" : "traces"}
                        {hasNextPage && " • Scroll to load more"}
                      </span>
                    </div>
                  )}
                </div>
              </div>
            </div>
          </Page.Body>
        </Page>
      </div>

      {/* Log Detail Sheet */}
      <LogDetailSheet
        log={selectedLog}
        open={!!selectedLog}
        onOpenChange={(open) => !open && setSelectedLog(null)}
      />
    </>
  );
}

function TraceListContent({
  error,
  isLoading,
  allTraces,
  searchQuery,
  expandedTraceId,
  isFetchingNextPage,
  onToggleExpand,
  onLogClick,
}: {
  error: Error | null;
  isLoading: boolean;
  allTraces: ToolCallSummary[];
  searchQuery: string | null;
  expandedTraceId: string | null;
  isFetchingNextPage: boolean;
  onToggleExpand: (traceId: string) => void;
  onLogClick: (log: TelemetryLogRecord) => void;
}) {
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
