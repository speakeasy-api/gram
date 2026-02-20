import {
  InsightsSidebar,
  useInsightsState,
} from "@/components/insights-sidebar";
import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { Page } from "@/components/page-layout";
import { SearchBar } from "@/components/ui/search-bar";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { cn } from "@/lib/utils";
import { telemetrySearchToolCalls } from "@gram/client/funcs/telemetrySearchToolCalls";
import {
  FeatureName,
  TelemetryLogRecord,
  ToolCallSummary,
} from "@gram/client/models/components";
import {
  useFeaturesSetMutation,
  useGramContext,
  useListToolsets,
} from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { useInfiniteQuery } from "@tanstack/react-query";
import { Check, ChevronDown, XIcon } from "lucide-react";
import { McpIcon } from "@/components/ui/mcp-icon";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { LogDetailSheet } from "./LogDetailSheet";
import { TraceRow } from "./TraceRow";
import {
  TimeRangePicker,
  type DateRangePreset,
  getPresetRange,
} from "@gram-ai/elements";
import { useSlugs } from "@/contexts/Sdk";

const perPage = 25;

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

/**
 * Format a date as a local ISO-like string (YYYY-MM-DDTHH:mm:ss) without UTC conversion.
 */
function toLocalISOString(date: Date): string {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  const hours = String(date.getHours()).padStart(2, "0");
  const minutes = String(date.getMinutes()).padStart(2, "0");
  const seconds = String(date.getSeconds()).padStart(2, "0");
  return `${year}-${month}-${day}T${hours}:${minutes}:${seconds}`;
}

/**
 * Parse a date string, treating it as local time if no timezone is specified.
 */
function parseLocalDate(dateStr: string): Date {
  if (dateStr.endsWith("Z") || /[+-]\d{2}:\d{2}$/.test(dateStr)) {
    const match = dateStr.match(
      /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})/,
    );
    if (match) {
      const [, year, month, day, hours, minutes, seconds] = match;
      return new Date(
        parseInt(year),
        parseInt(month) - 1,
        parseInt(day),
        parseInt(hours),
        parseInt(minutes),
        parseInt(seconds),
      );
    }
    return new Date(dateStr);
  }
  return new Date(dateStr);
}

/**
 * MCP Server filter dropdown
 */
function MCPServerFilter({
  selectedServer,
  onServerChange,
  toolsets,
  isLoading,
  disabled,
}: {
  selectedServer: string | null;
  onServerChange: (serverId: string | null) => void;
  toolsets: Array<{ slug: string; name: string }>;
  isLoading?: boolean;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);

  const selectedToolset = toolsets.find((t) => t.slug === selectedServer);
  const displayLabel = selectedToolset?.name ?? "All Servers";

  return (
    <div
      className={`flex items-center gap-2 ${disabled ? "opacity-50 pointer-events-none" : ""}`}
    >
      <div className="flex items-center h-[42px] bg-muted/50 rounded-md p-1 border border-border">
        <div className="flex items-center gap-1.5 h-8 px-3">
          <McpIcon className="size-3.5 text-muted-foreground" />
          <span className="text-sm font-medium text-foreground">Server</span>
        </div>
        <div className="w-px h-6 bg-border/50 mx-1" />
        <Popover open={!disabled && open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <button
              disabled={disabled || isLoading}
              className={`h-8 min-w-[140px] flex items-center justify-between gap-2 text-sm px-2 rounded transition-colors ${
                disabled || isLoading
                  ? "opacity-40 cursor-not-allowed"
                  : "hover:bg-muted/50"
              }`}
            >
              <span className="truncate max-w-[120px]">
                {isLoading ? "Loading..." : displayLabel}
              </span>
              <ChevronDown className="size-3.5 text-muted-foreground shrink-0" />
            </button>
          </PopoverTrigger>
          <PopoverContent className="w-[220px] p-0" align="end">
            <Command>
              <CommandInput placeholder="Search servers..." className="h-9" />
              <CommandList>
                <CommandEmpty>No servers found.</CommandEmpty>
                <CommandGroup>
                  <CommandItem
                    value="__all__"
                    onSelect={() => {
                      onServerChange(null);
                      setOpen(false);
                    }}
                    className="cursor-pointer"
                  >
                    <Check
                      className={`mr-2 size-4 ${selectedServer === null ? "opacity-100" : "opacity-0"}`}
                    />
                    <span>All Servers</span>
                  </CommandItem>
                  {toolsets.map((toolset) => (
                    <CommandItem
                      key={toolset.slug}
                      value={toolset.name}
                      onSelect={() => {
                        onServerChange(toolset.slug);
                        setOpen(false);
                      }}
                      className="cursor-pointer"
                    >
                      <Check
                        className={`mr-2 size-4 ${selectedServer === toolset.slug ? "opacity-100" : "opacity-0"}`}
                      />
                      <span className="truncate">{toolset.name}</span>
                    </CommandItem>
                  ))}
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      </div>
    </div>
  );
}

export default function LogsPage() {
  return <LogsContent />;
}

function LogsContent() {
  // Copilot config - filter to logs-related tools only
  const logsToolFilter = useCallback(
    ({ toolName }: { toolName: string }) =>
      toolName.toLowerCase().includes("logs"),
    [],
  );
  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: logsToolFilter,
  });
  const [searchParams, setSearchParams] = useSearchParams();
  const { projectSlug } = useSlugs();

  // Fetch toolsets for MCP server filter
  const { data: toolsetsData, isLoading: isLoadingToolsets } =
    useListToolsets();
  const toolsets = toolsetsData?.toolsets ?? [];

  // Initialize search and server filter from URL params
  const initialQuery = searchParams.get("q");
  const initialServer = searchParams.get("server");
  const [searchQuery, setSearchQuery] = useState<string | null>(
    initialQuery || null,
  );
  const [searchInput, setSearchInput] = useState(initialQuery || "");
  const [selectedServer, setSelectedServer] = useState<string | null>(
    initialServer || null,
  );
  const [expandedTraceId, setExpandedTraceId] = useState<string | null>(null);
  const [selectedLog, setSelectedLog] = useState<TelemetryLogRecord | null>(
    null,
  );
  const containerRef = useRef<HTMLDivElement>(null);

  // Time range from URL params
  const urlRange = searchParams.get("range");
  const urlFrom = searchParams.get("from");
  const urlTo = searchParams.get("to");

  const dateRange: DateRangePreset = isValidPreset(urlRange) ? urlRange : "30d";

  const customRange = useMemo(() => {
    if (urlFrom && urlTo) {
      const from = parseLocalDate(urlFrom);
      const to = parseLocalDate(urlTo);
      if (!isNaN(from.getTime()) && !isNaN(to.getTime())) {
        return { from, to };
      }
    }
    return null;
  }, [urlFrom, urlTo]);

  // Calculate actual from/to dates
  const { from, to } = useMemo(
    () => customRange ?? getPresetRange(dateRange),
    [customRange, dateRange],
  );

  // Time range update handlers
  const setDateRangeParam = useCallback(
    (preset: DateRangePreset) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set("range", preset);
          next.delete("from");
          next.delete("to");
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const setCustomRangeParam = useCallback(
    (from: Date, to: Date) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.delete("range");
          next.set("from", toLocalISOString(from));
          next.set("to", toLocalISOString(to));
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const clearCustomRange = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete("from");
        next.delete("to");
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  const client = useGramContext();

  // Derive URN prefix from selected server's toolUrns
  const serverUrnPrefix = useMemo(() => {
    if (!selectedServer) return null;
    const selectedToolset = toolsets.find((t) => t.slug === selectedServer);
    if (selectedToolset?.toolUrns?.length) {
      // Extract common URN prefix from the first tool URN
      // e.g., "tools:http:gram:some_tool" -> "tools:http:gram"
      const firstUrn = selectedToolset.toolUrns[0];
      const parts = firstUrn.split(":");
      if (parts.length >= 3) {
        // Take first 3 parts: "tools:http:gram"
        return parts.slice(0, 3).join(":");
      }
    }
    return null;
  }, [selectedServer, toolsets]);

  // Combine search query with server filter - server filter takes precedence
  const effectiveGramUrn = useMemo(() => {
    if (serverUrnPrefix) {
      // If we have a server filter, use its URN prefix
      // If there's also a search query, it should be combined or treated as additional filter
      return serverUrnPrefix;
    }
    return searchQuery;
  }, [serverUrnPrefix, searchQuery]);

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
        effectiveGramUrn,
        from.toISOString(),
        to.toISOString(),
      ],
      queryFn: ({ pageParam }) =>
        unwrapAsync(
          telemetrySearchToolCalls(client, {
            searchToolCallsPayload: {
              filter: {
                ...(effectiveGramUrn ? { gramUrn: effectiveGramUrn } : {}),
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

  // Handler for server filter change
  const handleServerChange = useCallback(
    (serverSlug: string | null) => {
      setSelectedServer(serverSlug);
      // Sync URL params preserving search query
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (serverSlug) {
            next.set("server", serverSlug);
          } else {
            next.delete("server");
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // Debounce search input and sync URL params
  useEffect(() => {
    const timeoutId = setTimeout(() => {
      const newQuery = searchInput || null;
      setSearchQuery(newQuery);
      // Sync URL params preserving server filter
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (newQuery) {
            next.set("q", newQuery);
          } else {
            next.delete("q");
          }
          return next;
        },
        { replace: true },
      );
    }, 500);
    return () => clearTimeout(timeoutId);
  }, [searchInput, setSearchParams]);

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
        selectedServer={selectedServer}
        onServerChange={handleServerChange}
        toolsets={toolsets}
        isLoadingToolsets={isLoadingToolsets}
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
        refetch={refetch}
        dateRange={dateRange}
        customRange={customRange}
        onPresetChange={setDateRangeParam}
        onCustomRangeChange={setCustomRangeParam}
        onClearCustomRange={clearCustomRange}
        projectSlug={projectSlug ?? ""}
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
  selectedServer,
  onServerChange,
  toolsets,
  isLoadingToolsets,
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
  refetch,
  dateRange,
  customRange,
  onPresetChange,
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
  selectedServer: string | null;
  onServerChange: (serverSlug: string | null) => void;
  toolsets: Array<{ slug: string; name: string; toolUrns: string[] }>;
  isLoadingToolsets?: boolean;
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
  refetch: () => void;
  dateRange: DateRangePreset;
  customRange: { from: Date; to: Date } | null;
  onPresetChange: (preset: DateRangePreset) => void;
  onCustomRangeChange: (from: Date, to: Date) => void;
  onClearCustomRange: () => void;
  projectSlug: string;
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
                  <div className="shrink-0">
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
                  </div>
                </div>
                {/* Filter and Search Row */}
                <div className="flex items-center gap-4">
                  <TimeRangePicker
                    preset={customRange ? null : dateRange}
                    customRange={customRange}
                    onPresetChange={onPresetChange}
                    onCustomRangeChange={onCustomRangeChange}
                    onClearCustomRange={onClearCustomRange}
                    projectSlug={projectSlug ?? ""}
                  />
                  <MCPServerFilter
                    selectedServer={selectedServer}
                    onServerChange={onServerChange}
                    toolsets={toolsets}
                    isLoading={isLoadingToolsets}
                  />
                  <SearchBar
                    value={searchInput}
                    onChange={setSearchInput}
                    placeholder="Search by tool URN"
                    className="flex-1 max-w-md"
                  />
                </div>
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
