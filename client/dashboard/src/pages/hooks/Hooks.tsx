import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { EnterpriseGate } from "@/components/enterprise-gate";
import { InsightsSidebar } from "@/components/insights-sidebar";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { SearchBar } from "@/components/ui/search-bar";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useSlugs } from "@/contexts/Sdk";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { cn } from "@/lib/utils";
import {
  getPresetRange,
  TimeRangePicker,
  type DateRangePreset,
} from "@gram-ai/elements";
import { telemetryGetHooksSummary } from "@gram/client/funcs/telemetryGetHooksSummary";
import { telemetrySearchLogs } from "@gram/client/funcs/telemetrySearchLogs";
import type {
  GetHooksSummaryResult,
  HooksServerSummary,
  LogFilter,
  TelemetryLogRecord,
  ToolCallSummary,
} from "@gram/client/models/components";
import { useGramContext } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Icon } from "@speakeasy-api/moonshine";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Filter, Settings } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { LogDetailSheet } from "../logs/LogDetailSheet";
import { TraceLogsList } from "../logs/TraceLogsList";
import { HooksEmptyState } from "./HooksEmptyState";
import { HookSourceIcon } from "./HookSourceIcon";

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

interface HookTrace extends ToolCallSummary {
  userEmail?: string;
  hookSource?: string;
}

function safeBase64Encode(str: string): string {
  try {
    return btoa(str);
  } catch {
    return btoa(encodeURIComponent(str));
  }
}

function safeBase64Decode(str: string): string | null {
  try {
    const decoded = atob(str);
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

export default function HooksPage() {
  return <HooksContent />;
}

function HooksContent() {
  const [searchParams, setSearchParams] = useSearchParams();
  const { projectSlug } = useSlugs();

  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: ({ toolName }) =>
      toolName.includes("logs") || toolName.includes("hooks"),
  });

  const initialServer = searchParams.get("server");
  const initialUserEmail = searchParams.get("user");
  const initialHideLocal = searchParams.get("hideLocal") !== "false"; // default to true
  const [serverFilter, setServerFilter] = useState<string | null>(
    initialServer || null,
  );
  const [serverInput, setServerInput] = useState(initialServer || "");
  const [userEmailFilter, setUserEmailFilter] = useState<string | null>(
    initialUserEmail || null,
  );
  const [userEmailInput, setUserEmailInput] = useState(initialUserEmail || "");
  const [hideLocalToolCalls, setHideLocalToolCalls] =
    useState(initialHideLocal);
  const [summaryView, setSummaryView] = useState<"servers" | "users">(
    "servers",
  );
  const [expandedTraceId, setExpandedTraceId] = useState<string | null>(null);
  const [selectedLog, setSelectedLog] = useState<TelemetryLogRecord | null>(
    null,
  );
  const containerRef = useRef<HTMLDivElement>(null);

  const client = useGramContext();

  const urlRange = searchParams.get("range");
  const urlFrom = searchParams.get("from");
  const urlTo = searchParams.get("to");
  const urlLabelEncoded = searchParams.get("label");
  const urlLabel = useMemo(() => {
    if (!urlLabelEncoded) return null;
    return safeBase64Decode(urlLabelEncoded);
  }, [urlLabelEncoded]);

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

  const { from, to } = useMemo(
    () => customRange ?? getPresetRange(dateRange),
    [customRange, dateRange],
  );

  // Fetch hooks summary
  const {
    data: summaryData,
    refetch: refetchSummary,
    isLogsDisabled: isSummaryLogsDisabled,
  } = useLogsEnabledErrorCheck(
    useQuery({
      queryKey: ["hooks-summary", from.toISOString(), to.toISOString()],
      queryFn: () =>
        unwrapAsync(
          telemetryGetHooksSummary(client, {
            getProjectMetricsSummaryPayload: {
              from,
              to,
            },
          }),
        ),
      throwOnError: false,
    }),
  );

  // Build attribute filters for server and user email
  const logFilters = useMemo(() => {
    const filters: LogFilter[] = [];

    // Filter by tool source (gram.tool_call.source)
    if (serverFilter) {
      filters.push({
        path: "gram.tool_call.source",
        operator: "contains",
        values: [serverFilter],
      });
    }

    // Filter by user email
    if (userEmailFilter) {
      filters.push({
        path: "user.email",
        operator: "contains",
        values: [userEmailFilter],
      });
    }

    // Hide local tool calls (filter out empty gram.tool_call.source)
    if (hideLocalToolCalls) {
      filters.push({
        path: "gram.tool_call.source",
        operator: "not_eq",
        values: [""],
      });
    }

    return filters.length > 0 ? filters : undefined;
  }, [serverFilter, userEmailFilter, hideLocalToolCalls]);

  // Fetch hooks logs with infinite scroll
  const {
    data: logsData,
    error,
    fetchNextPage,
    hasNextPage,
    isFetching,
    isFetchingNextPage,
    refetch: refetchLogs,
    isLogsDisabled: isLogsLogsDisabled,
  } = useLogsEnabledErrorCheck(
    useInfiniteQuery({
      queryKey: [
        "hooks-logs",
        serverFilter,
        userEmailFilter,
        hideLocalToolCalls,
        from.toISOString(),
        to.toISOString(),
      ],
      queryFn: ({ pageParam }) =>
        unwrapAsync(
          telemetrySearchLogs(client, {
            searchLogsPayload: {
              from,
              to,
              filters: logFilters,
              filter: { eventSource: "hook" },
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

  const logs = logsData?.pages.flatMap((page) => page.logs) ?? [];

  // Group logs by trace ID to create ToolCallSummary objects with extra metadata
  const groupedTraces = useMemo(() => {
    const traceMap = new Map<string, HookTrace>();

    for (const log of logs) {
      const traceId = log.traceId;
      if (!traceId) continue;

      const existing = traceMap.get(traceId);
      if (existing) {
        // Update existing trace
        existing.logCount++;

        // Update status based on hook event
        const hookEvent = log.attributes?.gram?.hook?.event as
          | string
          | undefined;
        if (hookEvent === "PostToolUseFailure") {
          existing.httpStatusCode = 500; // Mark as failure
        } else if (hookEvent === "PostToolUse" && !existing.httpStatusCode) {
          existing.httpStatusCode = 200; // Mark as success
        }

        // Track earliest timestamp
        if (BigInt(log.timeUnixNano) < BigInt(existing.startTimeUnixNano)) {
          existing.startTimeUnixNano = log.timeUnixNano;
        }
      } else {
        // Create new trace summary
        const hookEvent = log.attributes?.gram?.hook?.event as
          | string
          | undefined;
        const toolName = log.attributes?.gram?.tool?.name as string | undefined;
        const serverName = log.attributes?.gram?.tool_call?.source as
          | string
          | undefined;
        const userEmail = log.attributes?.user?.email as string | undefined;
        const hookSource = log.attributes?.gram?.hook?.source as
          | string
          | undefined;

        // Determine initial status
        let httpStatusCode: number | undefined;
        if (hookEvent === "PostToolUseFailure") {
          httpStatusCode = 500;
        } else if (hookEvent === "PostToolUse") {
          httpStatusCode = 200;
        }

        traceMap.set(traceId, {
          traceId,
          gramUrn: toolName || "",
          toolName: toolName,
          toolSource: serverName,
          logCount: 1,
          startTimeUnixNano: log.timeUnixNano,
          httpStatusCode,
          eventSource: "hook",
          userEmail,
          hookSource,
        });
      }
    }

    // Sort by timestamp descending
    return Array.from(traceMap.values()).sort((a, b) =>
      a.startTimeUnixNano < b.startTimeUnixNano ? 1 : -1,
    );
  }, [logsData]);

  const updateServerFilter = useCallback((value: string, immediate = false) => {
    const newServer = value || null;
    setServerInput(value);

    const applyFilter = () => {
      setServerFilter(newServer);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (newServer) {
            next.set("server", newServer);
          } else {
            next.delete("server");
          }
          return next;
        },
        { replace: true },
      );
    };

    if (immediate) {
      applyFilter();
    } else {
      // Will be handled by the debounced effect
    }
  }, [setSearchParams]);

  const updateUserFilter = useCallback((value: string, immediate = false) => {
    const newUserEmail = value || null;
    setUserEmailInput(value);

    const applyFilter = () => {
      setUserEmailFilter(newUserEmail);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (newUserEmail) {
            next.set("user", newUserEmail);
          } else {
            next.delete("user");
          }
          return next;
        },
        { replace: true },
      );
    };

    if (immediate) {
      applyFilter();
    } else {
      // Will be handled by the debounced effect
    }
  }, [setSearchParams]);

  useEffect(() => {
    const timeoutId = setTimeout(() => {
      const newServer = serverInput || null;
      setServerFilter(newServer);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (newServer) {
            next.set("server", newServer);
          } else {
            next.delete("server");
          }
          return next;
        },
        { replace: true },
      );
    }, 500);
    return () => clearTimeout(timeoutId);
  }, [serverInput, setSearchParams]);

  useEffect(() => {
    const timeoutId = setTimeout(() => {
      const newUserEmail = userEmailInput || null;
      setUserEmailFilter(newUserEmail);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (newUserEmail) {
            next.set("user", newUserEmail);
          } else {
            next.delete("user");
          }
          return next;
        },
        { replace: true },
      );
    }, 500);
    return () => clearTimeout(timeoutId);
  }, [userEmailInput, setSearchParams]);

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

  const handleLogClick = (log: TelemetryLogRecord) => {
    setSelectedLog(log);
  };

  const toggleExpand = (traceId: string) => {
    setExpandedTraceId((prev) => (prev === traceId ? null : traceId));
  };

  const handleHideLocalToggle = useCallback(
    (value: boolean) => {
      setHideLocalToolCalls(value);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (value) {
            next.delete("hideLocal"); // default is true, so remove param
          } else {
            next.set("hideLocal", "false");
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const refetch = useCallback(() => {
    refetchSummary();
    refetchLogs();
  }, [refetchSummary, refetchLogs]);

  const isLogsDisabled = isSummaryLogsDisabled || isLogsLogsDisabled;
  const isLoading = isFetching && groupedTraces.length === 0;

  return (
    <InsightsSidebar
      mcpConfig={mcpConfig}
      title="Explore Hooks"
      subtitle="Ask me about your hooks! Powered by Elements + Gram MCP"
      hideTrigger={isLogsDisabled}
    >
      <div className="h-full overflow-hidden flex flex-col">
        <Page>
          <Page.Header>
            <Page.Header.Breadcrumbs fullWidth />
          </Page.Header>
          <Page.Body fullWidth noPadding overflowHidden className="flex-1">
            <EnterpriseGate
              icon="workflow"
              description="Hooks are available on the Enterprise plan. Book a time to get started."
            >
              <HooksInnerContent
                isLogsDisabled={isLogsDisabled}
                isLoading={isLoading}
                isFetching={isFetching}
                error={error}
                summaryData={summaryData}
                summaryView={summaryView}
                onSummaryViewChange={setSummaryView}
                groupedTraces={groupedTraces}
                serverInput={serverInput}
                setServerInput={setServerInput}
                updateServerFilter={updateServerFilter}
                userEmailInput={userEmailInput}
                setUserEmailInput={setUserEmailInput}
                updateUserFilter={updateUserFilter}
                userEmailFilter={userEmailFilter}
                serverFilter={serverFilter}
                hideLocalToolCalls={hideLocalToolCalls}
                onHideLocalToggle={handleHideLocalToggle}
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
                dateRange={dateRange}
                customRange={customRange}
                customRangeLabel={urlLabel}
                onDateRangeChange={setDateRangeParam}
                onCustomRangeChange={setCustomRangeParam}
                onClearCustomRange={clearCustomRange}
                projectSlug={projectSlug}
              />
            </EnterpriseGate>
          </Page.Body>
        </Page>
      </div>
    </InsightsSidebar>
  );
}

function HooksInnerContent({
  isLogsDisabled,
  isLoading,
  isFetching,
  error,
  summaryData,
  summaryView,
  onSummaryViewChange,
  groupedTraces,
  serverInput,
  setServerInput,
  updateServerFilter,
  userEmailInput,
  setUserEmailInput,
  updateUserFilter,
  userEmailFilter,
  serverFilter,
  hideLocalToolCalls,
  onHideLocalToggle,
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
  summaryData?: GetHooksSummaryResult;
  summaryView: "servers" | "users";
  onSummaryViewChange: (view: "servers" | "users") => void;
  groupedTraces: HookTrace[];
  serverInput: string;
  setServerInput: (value: string) => void;
  updateServerFilter: (value: string, immediate?: boolean) => void;
  userEmailInput: string;
  setUserEmailInput: (value: string) => void;
  updateUserFilter: (value: string, immediate?: boolean) => void;
  userEmailFilter: string | null;
  serverFilter: string | null;
  hideLocalToolCalls: boolean;
  onHideLocalToggle: (value: boolean) => void;
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
  dateRange: DateRangePreset;
  customRange: { from: Date; to: Date } | null;
  customRangeLabel: string | null;
  onDateRangeChange: (preset: DateRangePreset) => void;
  onCustomRangeChange: (from: Date, to: Date, label?: string) => void;
  onClearCustomRange: () => void;
  projectSlug?: string;
}) {
  if (isLogsDisabled) {
    return (
      <div className="space-y-6">
        <div className="flex flex-col gap-1 min-w-0">
          <h1 className="text-xl font-semibold">Hooks</h1>
          <p className="text-sm text-muted-foreground">
            Monitor hook events and tool executions across all servers
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
      </div>
    );
  }

  return (
    <>
      <div className="flex flex-col flex-1 min-h-0 w-full">
        {/* Header section */}
        <div className="px-8 pt-8 pb-4 shrink-0">
          <div className="flex items-start justify-between gap-4 mb-4">
            <div className="flex flex-col gap-1 min-w-0">
              <h1 className="text-xl font-semibold">Hooks</h1>
              <p className="text-sm text-muted-foreground">
                Monitor hook events and tool executions across all servers
              </p>
            </div>
            <Button variant="outline" size="sm" asChild>
              <Link to="../settings/logs">
                <Settings className="h-4 w-4" />
                Configure settings
              </Link>
            </Button>
          </div>

          {/* Summary Tables */}
          {summaryData &&
            (summaryData.servers.length > 0 ||
              (summaryData.users && summaryData.users.length > 0)) && (
              <div className="mb-4 border rounded-lg overflow-hidden">
                {summaryView === "servers" && summaryData.servers.length > 0 && (
                  <HooksServerTable
                    servers={summaryData.servers}
                    onServerChange={(server) => updateServerFilter(server || "", true)}
                    summaryView={summaryView}
                    onSummaryViewChange={onSummaryViewChange}
                  />
                )}
                {summaryView === "users" &&
                  summaryData.users &&
                  summaryData.users.length > 0 && (
                    <HooksUserTable
                      users={summaryData.users}
                      onUserChange={(email) => updateUserFilter(email || "", true)}
                      summaryView={summaryView}
                      onSummaryViewChange={onSummaryViewChange}
                    />
                  )}
              </div>
            )}

          {/* Filter and Search Row */}
          <div className="flex items-center gap-2 flex-wrap">
            <SearchBar
              value={serverInput}
              onChange={setServerInput}
              placeholder="Filter by server name"
              className="flex-1 min-w-[200px]"
            />
            <SearchBar
              value={userEmailInput}
              onChange={setUserEmailInput}
              placeholder="Filter by user email"
              className="flex-1 min-w-[200px]"
            />
            <Button
              variant="outline"
              size="sm"
              onClick={() => onHideLocalToggle(!hideLocalToolCalls)}
              className={cn(
                "shrink-0 h-[42px]",
                hideLocalToolCalls ? "bg-primary/5" : "",
              )}
            >
              <Icon
                name={hideLocalToolCalls ? "eye-off" : "eye"}
                className="size-4"
              />
              {hideLocalToolCalls ? "Hiding local" : "Showing local"}
            </Button>
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

        {/* Content section */}
        <div className="flex-1 overflow-hidden min-h-0 border-t">
          <div className="h-full flex flex-col bg-background">
            {isFetching && groupedTraces.length > 0 && (
              <div className="absolute top-0 left-0 right-0 h-1 bg-primary/20 z-20">
                <div className="h-full bg-primary animate-pulse" />
              </div>
            )}

            {/* Header */}
            <div className="flex items-center gap-3 px-5 py-2.5 bg-muted/30 border-b text-xs font-medium text-muted-foreground uppercase tracking-wide shrink-0">
              <div className="shrink-0 w-[150px]">Timestamp</div>
              <div className="shrink-0 w-5" />
              <div className="flex-1 min-w-0">Server / Tool</div>
              <div className="shrink-0 w-[260px]">User</div>
              <div className="shrink-0 w-[120px]">Source</div>
              <div className="shrink-0 w-20 text-right">Status</div>
            </div>

            {/* Scrollable trace list */}
            <div
              ref={containerRef}
              className="overflow-y-auto flex-1"
              onScroll={handleScroll}
            >
              <HooksTraceContent
                error={error}
                isLoading={isLoading}
                groupedTraces={groupedTraces}
                serverFilter={serverFilter}
                userEmailFilter={userEmailFilter}
                expandedTraceId={expandedTraceId}
                isFetchingNextPage={isFetchingNextPage}
                onToggleExpand={toggleExpand}
                onLogClick={handleLogClick}
              />
            </div>

            {/* Footer */}
            {groupedTraces.length > 0 && (
              <div className="flex items-center gap-4 px-5 py-3 bg-muted/30 border-t text-sm text-muted-foreground shrink-0">
                <span>
                  {groupedTraces.length}{" "}
                  {groupedTraces.length === 1 ? "trace" : "traces"}
                  {hasNextPage && " • Scroll to load more"}
                </span>
              </div>
            )}
          </div>
        </div>
      </div>

      <LogDetailSheet
        log={selectedLog}
        open={!!selectedLog}
        onOpenChange={(open) => !open && setSelectedLog(null)}
      />
    </>
  );
}

interface SummaryItemData {
  name: string;
  displayName?: string;
  uniqueTools: number;
  failureRate: number;
}

interface SummaryTableProps {
  items: SummaryItemData[];
  onItemSelect: (key: string) => void;
  sortItems?: (items: SummaryItemData[]) => SummaryItemData[];
  tabValue?: string;
  onTabChange?: (value: string) => void;
  tabs?: Array<{ value: string; label: string }>;
}

function SummaryTable({
  items,
  selectedItemKey,
  onItemSelect,
  sortItems,
  tabValue,
  onTabChange,
  tabs,
}: SummaryTableProps) {
  const [isExpanded, setIsExpanded] = useState(false);
  const sortedItems = useMemo(() => {
    return sortItems ? sortItems([...items, ...items, ...items]) : items;
  }, [items, sortItems]);

  return (
    <div className="bg-background relative">
      {/* Header */}
      <div className="flex items-center gap-3 px-5 py-1 bg-muted/30 border-b text-xs font-medium text-muted-foreground uppercase tracking-wide">
        {/* First column - tabs or header */}
        <div className="flex-1 min-w-0">
          {tabs && tabValue && onTabChange ? (
            <Tabs value={tabValue} onValueChange={onTabChange}>
              <TabsList className="h-7 p-0.5">
                {tabs.map((tab) => (
                  <TabsTrigger
                    key={tab.value}
                    value={tab.value}
                    className="text-xs px-2.5 h-6"
                  >
                    {tab.label}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
          ) : (
            "Name"
          )}
        </div>
        <div className="shrink-0 w-[100px] text-right">Tool Calls</div>
        <div className="shrink-0 w-[100px] text-right">Success Rate</div>
        <div className="shrink-0 w-[80px] text-right">Status</div>
      </div>

      {/* Rows */}
      <div
        className={cn(
          "overflow-y-auto transition-all duration-300",
          isExpanded ? "max-h-[400px]" : "max-h-[150px]",
        )}
      >
        {sortedItems.map((item) => (
          <div
            key={item.name}
            className={cn(
              "group w-full flex items-center gap-3 px-5 py-3 border-b last:border-b-0 transition-colors",
              selectedItemKey === item.name ? "bg-primary/5" : "hover:bg-muted/50",
            )}
          >
            <button
              onClick={() =>
                onItemSelect(selectedItemKey === item.name ? null : item.name)
              }
              className="flex items-center gap-2 w-full text-left"
            >
              {/* Name + Actions */}
              <div className="flex items-center gap-2 flex-1 min-w-0">
                <span className="text-sm font-medium truncate">
                  {item.displayName || item.name}
                </span>

                {/* Actions - shown on hover */}
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onItemSelect(item.name);
                  }}
                  className="opacity-0 group-hover:opacity-100 p-1.5 rounded hover:bg-primary/10 transition-opacity shrink-0"
                  title={`Filter by ${item.displayName || item.name}`}
                >
                  <Filter className="size-4 text-muted-foreground hover:text-primary" />
                </button>
              </div>

              {/* Tools (despite the name, this is NOT the number of unique tools, but the number of tool calls) */}
              <div className="shrink-0 w-[100px] text-right text-sm text-muted-foreground">
                {item.uniqueTools}
              </div>

              {/* Success Rate */}
              <div className="shrink-0 w-[100px] text-right text-sm text-muted-foreground">
                {Math.round((1 - item.failureRate) * 100)}%
              </div>

              {/* Status */}
              <div className="shrink-0 w-[80px] flex justify-end">
                <Icon
                  name={item.failureRate > 0.1 ? "circle-alert" : "circle-check"}
                  className={cn(
                    "size-4 shrink-0",
                    item.failureRate > 0.1 ? "text-destructive" : "text-emerald-500",
                  )}
                />
              </div>
            </button>
          </div>
        ))}
      </div>

      {/* Expand/Collapse Button */}
      {sortedItems.length > 3 && (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => setIsExpanded(!isExpanded)}
          className="absolute bottom-2 left-1/2 -translate-x-1/2 h-7 px-2 bg-background/95 backdrop-blur-sm shadow-sm border border-border/50 hover:bg-muted"
        >
          <Icon
            name={isExpanded ? "chevrons-up" : "chevrons-down"}
            className="size-3.5"
          />
          <span className="text-xs ml-1">
            {isExpanded ? "Collapse" : "Expand"}
          </span>
        </Button>
      )}
    </div>
  );
}

function HooksServerTable({
  servers,
  selectedServer,
  onServerChange,
  summaryView,
  onSummaryViewChange,
}: {
  servers: HooksServerSummary[];
  selectedServer: string | null;
  onServerChange: (serverName: string | null) => void;
  summaryView: "servers" | "users";
  onSummaryViewChange: (view: "servers" | "users") => void;
}) {
  const items: SummaryItemData[] = servers.map((s) => ({
    name: s.serverName,
    displayName: !s.serverName ? "Local Tools" : s.serverName,
    uniqueTools: s.uniqueTools,
    failureRate: s.failureRate,
  }));

  return (
    <SummaryTable
      items={items}
      selectedItemKey={selectedServer}
      onItemSelect={onServerChange}
      sortItems={(items) =>
        items.sort((a, b) => {
          const aIsLocal = !a.name;
          const bIsLocal = !b.name;
          if (aIsLocal && !bIsLocal) return 1;
          if (!aIsLocal && bIsLocal) return -1;
          if (!aIsLocal && !bIsLocal) {
            return a.name.localeCompare(b.name);
          }
          return 0;
        })
      }
      tabValue={summaryView}
      onTabChange={(v) => onSummaryViewChange(v as "servers" | "users")}
      tabs={[
        { value: "servers", label: "Servers" },
        { value: "users", label: "Users" },
      ]}
    />
  );
}

function HooksUserTable({
  users,
  selectedUser,
  onUserChange,
  summaryView,
  onSummaryViewChange,
}: {
  users: Array<{
    userEmail: string;
    eventCount: number;
    uniqueTools: number;
    successCount: number;
    failureCount: number;
    failureRate: number;
  }>;
  selectedUser: string | null;
  onUserChange: (userEmail: string | null) => void;
  summaryView: "servers" | "users";
  onSummaryViewChange: (view: "servers" | "users") => void;
}) {
  const items: SummaryItemData[] = users.map((u) => ({
    name: u.userEmail,
    displayName:
      u.userEmail === "Unknown" || u.userEmail === ""
        ? "Unknown user"
        : u.userEmail,
    uniqueTools: u.uniqueTools,
    failureRate: u.failureRate,
  }));

  return (
    <SummaryTable
      items={items}
      selectedItemKey={selectedUser}
      onItemSelect={onUserChange}
      sortItems={(items) =>
        items.sort((a, b) => {
          const aCount = users.find((u) => u.userEmail === a.name)?.eventCount ?? 0;
          const bCount = users.find((u) => u.userEmail === b.name)?.eventCount ?? 0;
          return bCount - aCount;
        })
      }
      tabValue={summaryView}
      onTabChange={(v) => onSummaryViewChange(v as "servers" | "users")}
      tabs={[
        { value: "servers", label: "Servers" },
        { value: "users", label: "Users" },
      ]}
    />
  );
}

function HooksTraceContent({
  error,
  isLoading,
  groupedTraces,
  serverFilter,
  userEmailFilter,
  expandedTraceId,
  isFetchingNextPage,
  onToggleExpand,
  onLogClick,
}: {
  error: Error | null;
  isLoading: boolean;
  groupedTraces: HookTrace[];
  serverFilter: string | null;
  userEmailFilter: string | null;
  expandedTraceId: string | null;
  isFetchingNextPage: boolean;
  onToggleExpand: (traceId: string) => void;
  onLogClick: (log: TelemetryLogRecord) => void;
}) {
  if (error) {
    return (
      <div className="flex flex-col items-center gap-3 py-12">
        <div className="size-12 rounded-full bg-destructive/10 flex items-center justify-center">
          <Icon name="x" className="size-6 text-destructive" />
        </div>
        <span className="font-medium text-foreground">
          Error loading hook events
        </span>
        <span className="text-sm text-muted-foreground max-w-sm text-center">
          {error.message}
        </span>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center gap-2 py-12 text-muted-foreground">
        <Icon name="loader-circle" className="size-5 animate-spin" />
        <span>Loading hook events...</span>
      </div>
    );
  }

  if (groupedTraces.length === 0) {
    // Show the full empty state if no filters are applied
    const hasFilters = serverFilter || userEmailFilter;

    if (!hasFilters) {
      return <HooksEmptyState />;
    }

    // Show filtered empty state
    return (
      <div className="py-12 text-center">
        <div className="flex flex-col items-center gap-3">
          <div className="size-12 rounded-full bg-muted flex items-center justify-center">
            <Icon name="inbox" className="size-6 text-muted-foreground" />
          </div>
          <span className="font-medium text-foreground">
            No matching hook events
          </span>
          <span className="text-sm text-muted-foreground max-w-sm">
            Try adjusting your search query or time range
          </span>
        </div>
      </div>
    );
  }

  return (
    <>
      {groupedTraces.map((trace) => (
        <HookTraceRow
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
          <span className="text-sm">Loading more events...</span>
        </div>
      )}
    </>
  );
}

function HookTraceRow({
  trace,
  isExpanded,
  onToggle,
  onLogClick,
}: {
  trace: HookTrace;
  isExpanded: boolean;
  onToggle: () => void;
  onLogClick: (log: TelemetryLogRecord) => void;
}) {
  const timestamp = new Date(
    Number(BigInt(trace.startTimeUnixNano) / 1_000_000n),
  );
  const timeAgo = useMemo(() => {
    const now = new Date();
    const diff = now.getTime() - timestamp.getTime();
    const seconds = Math.floor(diff / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);

    if (days > 0) return `${days}d ago`;
    if (hours > 0) return `${hours}h ago`;
    if (minutes > 0) return `${minutes}m ago`;
    return `${seconds}s ago`;
  }, [timestamp]);

  const serverName = trace.toolSource;
  const toolName = trace.toolName;
  const userEmail = trace.userEmail;
  const hookSource = trace.hookSource;

  const serverNameBadge = useMemo(() => {
    const isLocal = !serverName;
    return (
      <span
        className={cn(
          "text-xs font-mono truncate px-2 py-1 rounded-md shrink-0",
          isLocal
            ? "bg-muted/50 text-muted-foreground"
            : "bg-primary/10 text-primary border border-primary/20 font-medium",
        )}
      >
        {serverName || "local"}
      </span>
    );
  }, [serverName]);

  const statusConfig = useMemo(() => {
    if (trace.httpStatusCode === 500) {
      return {
        color: "text-destructive",
        bgColor: "bg-destructive/10",
        label: "Failure",
      };
    } else if (trace.httpStatusCode === 200) {
      return {
        color: "text-emerald-500",
        bgColor: "bg-emerald-500/10",
        label: "Success",
      };
    }
    return {
      color: "text-muted-foreground",
      bgColor: "bg-muted",
      label: "Pending",
    };
  }, [trace.httpStatusCode]);

  return (
    <div className="border-b border-border/50 last:border-b-0">
      {/* Parent trace row */}
      <button
        onClick={onToggle}
        className="w-full flex items-center gap-3 px-5 py-2.5 hover:bg-muted/50 transition-colors text-left"
      >
        {/* Timestamp */}
        <div className="shrink-0 w-[150px] text-sm text-muted-foreground font-mono">
          {timeAgo}
        </div>

        {/* Expand/collapse indicator */}
        <div className="shrink-0 w-5 flex items-center justify-center">
          <Icon
            name={isExpanded ? "chevron-down" : "chevron-right"}
            className="size-4 text-muted-foreground"
          />
        </div>

        {/* Server badge + Tool name */}
        <div className="flex items-center gap-2 flex-1 min-w-0">
          {serverNameBadge}
          <span className="text-sm font-mono truncate">
            {toolName || "unknown"}
          </span>
        </div>

        {/* User email */}
        <div className="shrink-0 w-[260px] text-sm text-muted-foreground truncate">
          {userEmail || "—"}
        </div>

        {/* Hook source */}
        <div className="shrink-0 w-[120px] flex items-center gap-2">
          <HookSourceIcon source={hookSource} className="size-4 shrink-0" />
          {hookSource && (
            <span className="text-xs text-foreground font-medium truncate">
              {hookSource}
            </span>
          )}
        </div>

        {/* Status badge */}
        <div className="shrink-0 w-20 flex justify-end">
          <div
            className={cn(
              "inline-flex items-center gap-1.5 px-2 py-1 rounded-md text-xs font-medium",
              statusConfig.bgColor,
              statusConfig.color,
            )}
          >
            <div
              className={cn(
                "size-1.5 rounded-full",
                statusConfig.color === "text-muted-foreground"
                  ? "bg-muted-foreground"
                  : "bg-current",
              )}
            />
            {statusConfig.label}
          </div>
        </div>
      </button>

      {/* Expanded child logs */}
      {isExpanded && (
        <TraceLogsList
          traceId={trace.traceId}
          toolName={toolName || "unknown"}
          isExpanded={isExpanded}
          onLogClick={onLogClick}
          parentTimestamp={trace.startTimeUnixNano}
        />
      )}
    </div>
  );
}
