import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { InsightsConfig } from "@/components/insights-sidebar";
import { RequireScope } from "@/components/require-scope";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { McpIcon } from "@/components/ui/mcp-icon";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { useSlugs } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import {
  getPresetRange,
  TimeRangePicker,
  type DateRangePreset,
} from "@gram-ai/elements";
import { telemetrySearchToolCalls } from "@gram/client/funcs/telemetrySearchToolCalls";
import { Operator } from "@gram/client/models/components/logfilter";
import {
  TelemetryLogRecord,
  ToolCallSummary,
} from "@gram/client/models/components";
import {
  useGramContext,
  useListAttributeKeys,
  useListToolsets,
} from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Icon } from "@speakeasy-api/moonshine";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import {
  Check,
  ChevronDown,
  Loader2,
  RefreshCw,
  Settings,
  XIcon,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useOrgRoutes } from "@/routes";
import { Link, useSearchParams } from "react-router";
import { type ActiveLogFilter, applyFilterAdd } from "./log-filter-types";
import { parseFilters, serializeFilters } from "./log-filter-url";
import { LogDetailSheet } from "./LogDetailSheet";
import { LogFilterBar } from "./LogFilterBar";
import { TraceRow } from "./TraceRow";
import { TriggerLogRow } from "./TriggerLogRow";
import {
  useAttributeLogsQuery,
  type TraceSummary,
} from "./use-attribute-logs-query";

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

const perPage = 100;

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
      className={`flex items-center gap-2 ${disabled ? "pointer-events-none opacity-50" : ""}`}
    >
      <div className="border-border flex h-[42px] items-center rounded-md border p-1">
        <div className="flex h-8 items-center gap-1.5 px-3">
          <McpIcon className="text-muted-foreground size-3.5" />
          <span className="text-foreground text-sm font-medium">Server</span>
        </div>
        <div className="bg-border/50 mx-1 h-6 w-px" />
        <Popover open={!disabled && open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <button
              disabled={disabled || isLoading}
              className={`flex h-8 min-w-[140px] items-center justify-between gap-2 rounded px-2 text-sm transition-colors ${
                disabled || isLoading
                  ? "cursor-not-allowed opacity-40"
                  : "hover:bg-muted/50"
              }`}
            >
              <span className="max-w-[120px] truncate">
                {isLoading ? "Loading..." : displayLabel}
              </span>
              <ChevronDown className="text-muted-foreground size-3.5 shrink-0" />
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
  return (
    <RequireScope scope="project:read" level="page">
      <LogsContent />
    </RequireScope>
  );
}

function LogsContent() {
  const [searchParams, setSearchParams] = useSearchParams();
  const { projectSlug } = useSlugs();

  // Copilot config - filter to logs-related tools only
  const logsToolFilter = useCallback(({ toolName }: { toolName: string }) => {
    const name = toolName.toLowerCase();
    return name.includes("logs") || name.includes("attribute_keys");
  }, []);
  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: logsToolFilter,
  });

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

  // Attribute filter state from URL
  const [logFilters, setAttributeFilters] = useState<ActiveLogFilter[]>(() =>
    parseFilters(searchParams.get("af")),
  );

  const hasAttributeFilters = logFilters.length > 0;

  const serverFilters = useMemo(
    () =>
      selectedServer
        ? [
            {
              path: "gram.toolset.slug",
              operator: Operator.Eq,
              values: [selectedServer],
            },
          ]
        : [],
    [selectedServer],
  );

  const hasStructuredFilters = hasAttributeFilters || serverFilters.length > 0;

  const handleLogFiltersChange = useCallback(
    (filters: ActiveLogFilter[]) => {
      setAttributeFilters(filters);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          const serialized = serializeFilters(filters);
          if (serialized) {
            next.set("af", serialized);
          } else {
            next.delete("af");
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const effectiveGramUrn = searchQuery;

  // Fetch attribute keys for filter bar
  const { data: attributeKeysData, isLoading: isLoadingAttributeKeys } =
    useListAttributeKeys(
      { getProjectMetricsSummaryPayload: { from, to } },
      undefined,
      { throwOnError: false },
    );
  const attributeKeys = attributeKeysData?.keys ?? [];

  // Standard tool calls query (used when no structured filters are active)
  const toolCallsQuery = useLogsEnabledErrorCheck(
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
      enabled: !hasStructuredFilters,
      throwOnError: false,
    }),
  );

  // Logs query used when attribute filters or server filters are active.
  const attrLogsQuery = useLogsEnabledErrorCheck(
    useAttributeLogsQuery({
      logFilters,
      extraFilters: serverFilters,
      gramUrn: effectiveGramUrn,
      from,
      to,
      enabled: hasStructuredFilters,
    }),
  );

  // Pick the active query based on whether structured filters are active.
  const {
    data,
    error,
    fetchNextPage,
    hasNextPage,
    isFetching,
    isFetchingNextPage,
    refetch,
    isLogsDisabled,
  } = hasStructuredFilters ? attrLogsQuery : toolCallsQuery;

  const queryClient = useQueryClient();
  const handleRefresh = useCallback(() => {
    refetch();
    queryClient.invalidateQueries({ queryKey: ["trace-logs"] });
  }, [refetch, queryClient]);

  // Flatten all pages into a single array of traces, merging duplicates that
  // span page boundaries (attribute-filtered logs are grouped per-page, so the
  // same traceId can appear in multiple pages with partial counts).
  const allTraces = useMemo(() => {
    const raw = data?.pages.flatMap((page) => page.toolCalls) ?? [];
    if (!hasStructuredFilters) return raw;

    const merged = new Map<string, ToolCallSummary>();
    for (const trace of raw) {
      const existing = merged.get(trace.traceId);
      if (existing) {
        existing.logCount += trace.logCount;
        if (
          BigInt(trace.startTimeUnixNano) < BigInt(existing.startTimeUnixNano)
        ) {
          existing.startTimeUnixNano = trace.startTimeUnixNano;
        }
      } else {
        merged.set(trace.traceId, { ...trace });
      }
    }
    return Array.from(merged.values()).sort((a, b) =>
      a.startTimeUnixNano < b.startTimeUnixNano ? 1 : -1,
    );
  }, [data?.pages, hasStructuredFilters]);

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

  // Submit search explicitly (Enter / Tab / clear)
  const handleSearchSubmit = useCallback(
    (query: string) => {
      const newQuery = query || null;
      setSearchQuery(newQuery);
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
    },
    [setSearchParams],
  );

  // Handle scroll for infinite loading
  const handleScroll = useCallback(
    (e: React.UIEvent<HTMLDivElement>) => {
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
    },
    [fetchNextPage, hasNextPage, isFetching, isFetchingNextPage],
  );

  const toggleExpand = useCallback((traceId: string) => {
    setExpandedTraceId((prev) => (prev === traceId ? null : traceId));
  }, []);

  const handleLogClick = useCallback((log: TelemetryLogRecord) => {
    setSelectedLog(log);
  }, []);

  const isLoading = isFetching && allTraces.length === 0;
  const hasActiveFilters =
    !!selectedServer || !!searchQuery || logFilters.length > 0;

  return (
    <>
      <InsightsConfig
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
            prompt:
              "Search for recent error logs and summarize what's happening",
          },
        ]}
      />
      <LogsInnerContent
        isLogsDisabled={isLogsDisabled}
        isLoading={isLoading}
        isFetching={isFetching}
        error={error}
        allTraces={allTraces}
        searchInput={searchInput}
        setSearchInput={setSearchInput}
        onSearchSubmit={handleSearchSubmit}
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
        refetch={refetch}
        onRefresh={handleRefresh}
        // Time range props
        dateRange={dateRange}
        customRange={customRange}
        customRangeLabel={urlLabel}
        onDateRangeChange={setDateRangeParam}
        onCustomRangeChange={setCustomRangeParam}
        onClearCustomRange={clearCustomRange}
        projectSlug={projectSlug}
        // Attribute filter props
        logFilters={logFilters}
        onLogFiltersChange={handleLogFiltersChange}
        attributeKeys={attributeKeys}
        isLoadingAttributeKeys={isLoadingAttributeKeys}
        hasActiveFilters={hasActiveFilters}
      />
    </>
  );
}

function LogsInnerContent({
  isLogsDisabled,
  isLoading,
  isFetching,
  error,
  allTraces,
  searchInput,
  setSearchInput,
  onSearchSubmit,
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
  refetch,
  onRefresh,
  // Time range props
  dateRange,
  customRange,
  customRangeLabel,
  onDateRangeChange,
  onCustomRangeChange,
  onClearCustomRange,
  projectSlug,
  // Attribute filter props
  logFilters,
  onLogFiltersChange,
  attributeKeys,
  isLoadingAttributeKeys,
  hasActiveFilters,
}: {
  isLogsDisabled: boolean;
  isLoading: boolean;
  isFetching: boolean;
  error: Error | null;
  allTraces: ToolCallSummary[];
  searchInput: string;
  setSearchInput: (value: string) => void;
  onSearchSubmit: (query: string) => void;
  selectedServer: string | null;
  onServerChange: (serverSlug: string | null) => void;
  toolsets: Array<{ slug: string; name: string }>;
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
  refetch: () => void;
  onRefresh: () => void;
  // Time range props
  dateRange: DateRangePreset;
  customRange: { from: Date; to: Date } | null;
  customRangeLabel: string | null;
  onDateRangeChange: (preset: DateRangePreset) => void;
  onCustomRangeChange: (from: Date, to: Date, label?: string) => void;
  onClearCustomRange: () => void;
  projectSlug?: string;
  // Attribute filter props
  logFilters: ActiveLogFilter[];
  onLogFiltersChange: (filters: ActiveLogFilter[]) => void;
  attributeKeys: string[];
  isLoadingAttributeKeys?: boolean;
  hasActiveFilters: boolean;
}) {
  const orgRoutes = useOrgRoutes();

  const handleAddFilterFromLog = useCallback(
    (path: string, op: Operator, value: string) => {
      onLogFiltersChange(applyFilterAdd(logFilters, { path, op, value }));
    },
    [logFilters, onLogFiltersChange],
  );

  // Enforce a minimum visible duration for the refresh button's in-flight
  // state. Instant or cached refetches would otherwise flash the spinner for
  // a few ms and feel uncommunicative.
  const MIN_REFRESH_MS = 2000;
  const [refreshStartedAt, setRefreshStartedAt] = useState<number | null>(null);

  useEffect(() => {
    if (refreshStartedAt === null || isFetching) return;
    const remaining = Math.max(
      0,
      MIN_REFRESH_MS - (Date.now() - refreshStartedAt),
    );
    const timeout = setTimeout(() => setRefreshStartedAt(null), remaining);
    return () => clearTimeout(timeout);
  }, [isFetching, refreshStartedAt]);

  const isRefreshing = refreshStartedAt !== null;

  const handleRefreshClick = () => {
    setRefreshStartedAt(Date.now());
    onRefresh();
  };

  const pageTitle = (
    <div className="flex min-w-0 flex-col gap-1">
      <h1 className="text-xl font-semibold">Logs</h1>
      <p className="text-muted-foreground text-sm">
        Browse raw tool call traces and telemetry data
      </p>
    </div>
  );

  return (
    <>
      <div className="flex h-full flex-col overflow-hidden">
        <Page>
          <Page.Header>
            <Page.Header.Breadcrumbs fullWidth />
          </Page.Header>
          {isLogsDisabled ? (
            <Page.Body fullWidth className="space-y-6">
              {pageTitle}
              <div className="relative flex-1">
                <div
                  className="pointer-events-none h-full select-none"
                  aria-hidden="true"
                >
                  <ObservabilitySkeleton />
                </div>
                <EnableLoggingOverlay onEnabled={refetch} />
              </div>
            </Page.Body>
          ) : (
            <Page.Body fullWidth noPadding overflowHidden>
              <div className="flex min-h-0 w-full flex-1 flex-col">
                {/* Header section */}
                <div className="shrink-0 px-8 py-4">
                  <div className="mb-4 flex items-start justify-between gap-4">
                    {pageTitle}
                    <div className="flex items-center gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={handleRefreshClick}
                        disabled={isRefreshing}
                        aria-label="Refresh logs"
                        className={cn(
                          "min-w-[110px] justify-center transition-all",
                          isRefreshing &&
                            "ring-primary/30 ring-2 ring-offset-1",
                        )}
                      >
                        {isRefreshing ? (
                          <>
                            <Loader2 className="h-4 w-4 animate-spin" />
                            <span className="text-muted-foreground">
                              Refreshing…
                            </span>
                          </>
                        ) : (
                          <>
                            <RefreshCw className="h-4 w-4" />
                            Refresh
                          </>
                        )}
                      </Button>
                      <Button variant="outline" size="sm" asChild>
                        <Link to={orgRoutes.logs.href()}>
                          <Settings className="h-4 w-4" />
                          Configure settings
                        </Link>
                      </Button>
                    </div>
                  </div>
                  {/* Filter and Search Row */}
                  <div className="flex flex-wrap items-center gap-4">
                    <MCPServerFilter
                      selectedServer={selectedServer}
                      onServerChange={onServerChange}
                      toolsets={toolsets}
                      isLoading={isLoadingToolsets}
                    />
                    <div className="flex-1">
                      <LogFilterBar
                        filters={logFilters}
                        onChange={onLogFiltersChange}
                        attributeKeys={attributeKeys}
                        isLoadingKeys={isLoadingAttributeKeys}
                        searchInput={searchInput}
                        onSearchInputChange={setSearchInput}
                        onSearchSubmit={onSearchSubmit}
                      />
                    </div>
                    <div className="ml-auto">
                      <TimeRangePicker
                        preset={customRange ? null : dateRange}
                        customRange={customRange}
                        customRangeLabel={customRangeLabel}
                        onPresetChange={onDateRangeChange}
                        onCustomRangeChange={onCustomRangeChange}
                        onClearCustomRange={onClearCustomRange}
                        projectSlug={projectSlug}
                      />
                    </div>
                  </div>
                </div>

                {/* Content section - full width */}
                <div className="min-h-0 flex-1 overflow-hidden border-t">
                  <div className="bg-background flex h-full flex-col">
                    {/* Header */}
                    <div className="bg-muted/30 text-muted-foreground flex shrink-0 items-center gap-3 border-b px-8 py-2.5 text-xs font-medium tracking-wide uppercase">
                      <div className="w-1.5 shrink-0" />
                      <div className="w-[150px] shrink-0">Timestamp</div>
                      <div className="w-5 shrink-0" />
                      <div className="flex-1">Source / Tool</div>
                      <div className="w-16 shrink-0 text-right">Status</div>
                    </div>

                    {/* Scrollable trace list */}
                    <div
                      ref={containerRef}
                      className="flex-1 overflow-y-auto"
                      onScroll={handleScroll}
                    >
                      <TraceListContent
                        error={error}
                        isLoading={isLoading}
                        allTraces={allTraces}
                        hasActiveFilters={hasActiveFilters}
                        expandedTraceId={expandedTraceId}
                        isFetchingNextPage={isFetchingNextPage}
                        onToggleExpand={toggleExpand}
                        onLogClick={handleLogClick}
                      />
                    </div>

                    {/* Footer */}
                    {allTraces.length > 0 && (
                      <div className="bg-muted/30 text-muted-foreground flex shrink-0 items-center gap-4 border-t px-8 py-3 text-sm">
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
          )}
        </Page>
      </div>

      <LogDetailSheet
        log={selectedLog}
        open={!!selectedLog}
        onOpenChange={(open) => !open && setSelectedLog(null)}
        onAddFilter={handleAddFilterFromLog}
      />
    </>
  );
}

function TraceListContent({
  error,
  isLoading,
  allTraces,
  hasActiveFilters,
  expandedTraceId,
  isFetchingNextPage,
  onToggleExpand,
  onLogClick,
}: {
  error: Error | null;
  isLoading: boolean;
  allTraces: ToolCallSummary[];
  hasActiveFilters: boolean;
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
    return <LogsEmptyState hasActiveFilters={hasActiveFilters} />;
  }

  return (
    <>
      {allTraces.map((trace) =>
        (trace as TraceSummary).eventSource === "trigger" ? (
          <TriggerLogRow
            key={trace.traceId}
            trace={trace as TraceSummary}
            onLogClick={onLogClick}
          />
        ) : (
          <TraceRow
            key={trace.traceId}
            trace={trace}
            isExpanded={expandedTraceId === trace.traceId}
            onToggle={onToggleExpand}
            onLogClick={onLogClick}
          />
        ),
      )}

      {isFetchingNextPage && (
        <div className="text-muted-foreground flex items-center justify-center gap-2 border-t py-4">
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
      <div className="bg-destructive/10 flex size-12 items-center justify-center rounded-full">
        <XIcon className="text-destructive size-6" />
      </div>
      <span className="text-foreground font-medium">Error loading traces</span>
      <span className="text-muted-foreground max-w-sm text-center text-sm">
        {error.message}
      </span>
    </div>
  );
}

function LogsLoading() {
  return (
    <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
      <Icon name="loader-circle" className="size-5 animate-spin" />
      <span>Loading traces...</span>
    </div>
  );
}

function LogsEmptyState({ hasActiveFilters }: { hasActiveFilters: boolean }) {
  return (
    <div className="py-12 text-center">
      <div className="flex flex-col items-center gap-3">
        <div className="bg-muted flex size-12 items-center justify-center rounded-full">
          <Icon name="inbox" className="text-muted-foreground size-6" />
        </div>
        <span className="text-foreground font-medium">
          {hasActiveFilters ? "No matching traces" : "No traces found"}
        </span>
        <span className="text-muted-foreground max-w-sm text-sm">
          {hasActiveFilters
            ? "Try adjusting your search or filters"
            : "Traces will appear here when tool calls are made"}
        </span>
      </div>
    </div>
  );
}
