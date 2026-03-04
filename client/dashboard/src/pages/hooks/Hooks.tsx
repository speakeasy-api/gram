import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { EnterpriseGate } from "@/components/enterprise-gate";
import { InsightsSidebar } from "@/components/insights-sidebar";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { SearchBar } from "@/components/ui/search-bar";
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
  AttributeFilter,
  GetHooksSummaryResult,
  HooksServerSummary,
  TelemetryLogRecord,
} from "@gram/client/models/components";
import { useGramContext } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Icon } from "@speakeasy-api/moonshine";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Settings } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { LogDetailSheet } from "../logs/LogDetailSheet";
import { HooksEmptyState } from "./HooksEmptyState";

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

const perPage = 25;

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

  const initialQuery = searchParams.get("q");
  const initialServer = searchParams.get("server");
  const initialUserEmail = searchParams.get("user");
  const [searchQuery, setSearchQuery] = useState<string | null>(
    initialQuery || null,
  );
  const [searchInput, setSearchInput] = useState(initialQuery || "");
  const [selectedServer, setSelectedServer] = useState<string | null>(
    initialServer || null,
  );
  const [userEmailFilter, setUserEmailFilter] = useState<string | null>(
    initialUserEmail || null,
  );
  const [userEmailInput, setUserEmailInput] = useState(initialUserEmail || "");
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

  // Build attribute filters for server, search query, and user email
  const attributeFilters = useMemo(() => {
    const filters: AttributeFilter[] = [];

    // Filter by tool source (gram.tool_call.source)
    if (selectedServer) {
      filters.push({
        path: "gram.tool_call.source",
        op: "eq",
        value: selectedServer,
      });
    }

    // Filter by tool name (search query)
    if (searchQuery) {
      filters.push({
        path: "gram.tool.name",
        op: "contains",
        value: searchQuery,
      });
    }

    // Filter by user email
    if (userEmailFilter) {
      filters.push({
        path: "user.email",
        op: "contains",
        value: userEmailFilter,
      });
    }

    return filters.length > 0 ? filters : undefined;
  }, [selectedServer, searchQuery, userEmailFilter]);

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
        searchQuery,
        selectedServer,
        userEmailFilter,
        from.toISOString(),
        to.toISOString(),
      ],
      queryFn: ({ pageParam }) =>
        unwrapAsync(
          telemetrySearchLogs(client, {
            searchLogsPayload: {
              filter: {
                eventSource: "hook",
                from,
                to,
                attributeFilters,
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

  const logs = logsData?.pages.flatMap((page) => page.logs) ?? [];

  const handleServerChange = useCallback(
    (serverName: string | null) => {
      if (serverName === "") serverName = null;
      setSelectedServer(serverName);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (serverName) {
            next.set("server", serverName);
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

  useEffect(() => {
    const timeoutId = setTimeout(() => {
      const newQuery = searchInput || null;
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
    }, 500);
    return () => clearTimeout(timeoutId);
  }, [searchInput, setSearchParams]);

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

  const refetch = useCallback(() => {
    refetchSummary();
    refetchLogs();
  }, [refetchSummary, refetchLogs]);

  const isLogsDisabled = isSummaryLogsDisabled || isLogsLogsDisabled;
  const isLoading = isFetching && logs.length === 0;

  return (
    <InsightsSidebar
      mcpConfig={mcpConfig}
      title="Explore Hooks"
      subtitle="Ask me about your hooks! Powered by Elements + Gram MCP"
      hideTrigger={isLogsDisabled}
    >
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
          logs={logs}
          searchQuery={searchQuery}
          searchInput={searchInput}
          setSearchInput={setSearchInput}
          userEmailInput={userEmailInput}
          setUserEmailInput={setUserEmailInput}
          userEmailFilter={userEmailFilter}
          selectedServer={selectedServer}
          onServerChange={handleServerChange}
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
    </InsightsSidebar>
  );
}

function HooksInnerContent({
  isLogsDisabled,
  isLoading,
  isFetching,
  error,
  summaryData,
  logs,
  searchQuery,
  searchInput,
  setSearchInput,
  userEmailInput,
  setUserEmailInput,
  userEmailFilter,
  selectedServer,
  onServerChange,
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
  logs: TelemetryLogRecord[];
  searchQuery: string | null;
  searchInput: string;
  setSearchInput: (value: string) => void;
  userEmailInput: string;
  setUserEmailInput: (value: string) => void;
  userEmailFilter: string | null;
  selectedServer: string | null;
  onServerChange: (serverName: string | null) => void;
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
      <div className="h-full overflow-hidden flex flex-col">
        <Page>
          <Page.Header>
            <Page.Header.Breadcrumbs fullWidth />
          </Page.Header>
          <Page.Body fullWidth className="space-y-6">
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

                {/* Server Cards */}
                {summaryData && summaryData.servers.length > 0 && (
                  <div className="mb-4">
                    <HooksServerCards
                      servers={summaryData.servers}
                      selectedServer={selectedServer}
                      onServerChange={onServerChange}
                    />
                  </div>
                )}

                {/* Filter and Search Row */}
                <div className="flex items-center gap-4 flex-wrap">
                  <SearchBar
                    value={searchInput}
                    onChange={setSearchInput}
                    placeholder="Search by tool name"
                    className="flex-1 min-w-[200px]"
                  />
                  <SearchBar
                    value={userEmailInput}
                    onChange={setUserEmailInput}
                    placeholder="Filter by user email"
                    className="flex-1 min-w-[200px]"
                  />
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
                  {isFetching && logs.length > 0 && (
                    <div className="absolute top-0 left-0 right-0 h-1 bg-primary/20 z-20">
                      <div className="h-full bg-primary animate-pulse" />
                    </div>
                  )}

                  {/* Header */}
                  <div className="flex items-center gap-3 px-5 py-2.5 bg-muted/30 border-b text-xs font-medium text-muted-foreground uppercase tracking-wide shrink-0">
                    <div className="shrink-0 w-[150px]">Timestamp</div>
                    <div className="flex-1 min-w-0">Server / Tool</div>
                    <div className="shrink-0 w-[250px]">User</div>
                    <div className="shrink-0 w-20 text-right">Event</div>
                  </div>

                  {/* Scrollable logs list */}
                  <div
                    ref={containerRef}
                    className="overflow-y-auto flex-1"
                    onScroll={handleScroll}
                  >
                    <HooksLogsContent
                      error={error}
                      isLoading={isLoading}
                      logs={logs}
                      searchQuery={searchQuery}
                      selectedServer={selectedServer}
                      userEmailFilter={userEmailFilter}
                      isFetchingNextPage={isFetchingNextPage}
                      onLogClick={handleLogClick}
                    />
                  </div>

                  {/* Footer */}
                  {logs.length > 0 && (
                    <div className="flex items-center gap-4 px-5 py-3 bg-muted/30 border-t text-sm text-muted-foreground shrink-0">
                      <span>
                        {logs.length} {logs.length === 1 ? "event" : "events"}
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

      <LogDetailSheet
        log={selectedLog}
        open={!!selectedLog}
        onOpenChange={(open) => !open && setSelectedLog(null)}
      />
    </>
  );
}

function HooksServerCards({
  servers,
  selectedServer,
  onServerChange,
}: {
  servers: HooksServerSummary[];
  selectedServer: string | null;
  onServerChange: (serverName: string | null) => void;
}) {
  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
      {servers.map((server) => (
        <button
          key={server.serverName}
          onClick={() =>
            onServerChange(
              selectedServer === server.serverName ? null : server.serverName,
            )
          }
          className={cn(
            "p-4 rounded-lg border transition-all text-left",
            selectedServer === server.serverName
              ? "border-primary bg-primary/5"
              : "border-border hover:border-primary/50 hover:bg-muted/50",
          )}
        >
          <div className="flex items-start justify-between mb-2">
            <div className="font-medium text-sm truncate">
              {!server.serverName ? "Local Tools" : server.serverName}
            </div>
            <Icon
              name={server.failureRate > 0.1 ? "circle-alert" : "circle-check"}
              className={cn(
                "size-4 shrink-0",
                server.failureRate > 0.1
                  ? "text-destructive"
                  : "text-emerald-500",
              )}
            />
          </div>
          <div className="space-y-1">
            <div className="text-2xl font-semibold">{server.eventCount}</div>
            <div className="text-xs text-muted-foreground">
              {server.uniqueTools} {server.uniqueTools === 1 ? "tool" : "tools"}
              {" • "}
              {Math.round((1 - server.failureRate) * 100)}% success
            </div>
          </div>
        </button>
      ))}
    </div>
  );
}

function HooksLogsContent({
  error,
  isLoading,
  logs,
  searchQuery,
  selectedServer,
  userEmailFilter,
  isFetchingNextPage,
  onLogClick,
}: {
  error: Error | null;
  isLoading: boolean;
  logs: TelemetryLogRecord[];
  searchQuery: string | null;
  selectedServer: string | null;
  userEmailFilter: string | null;
  isFetchingNextPage: boolean;
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

  if (logs.length === 0) {
    // Show the full empty state if no filters are applied
    const hasFilters = searchQuery || selectedServer || userEmailFilter;

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
      {logs.map((log) => (
        <HookLogRow key={log.id} log={log} onClick={() => onLogClick(log)} />
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

function HookLogRow({
  log,
  onClick,
}: {
  log: TelemetryLogRecord;
  onClick: () => void;
}) {
  const hookEventName = log.attributes?.gram?.hook?.event as string | undefined;
  const toolName = log.attributes?.gram?.tool?.name as string | undefined;
  const serverName = log.attributes?.gram?.tool_call?.source as
    | string
    | undefined;
  const userEmail = log.attributes?.user?.email as string | undefined;

  const timestamp = new Date(Number(log.timeUnixNano) / 1000000);
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

  const serverNameBadge = useMemo(() => {
    return (
      <span className="text-xs font-mono truncate bg-muted px-1 py-1 rounded-sm">
        {serverName || "local"}
      </span>
    );
  }, [serverName]);

  return (
    <button
      onClick={onClick}
      className="w-full flex items-center gap-3 px-5 py-3 border-b hover:bg-muted/30 transition-colors text-left"
    >
      <div className="shrink-0 w-[150px] text-sm text-muted-foreground">
        {timeAgo}
      </div>
      <div className="flex-1 min-w-0 flex items-center gap-2">
        {serverNameBadge}
        <span className="text-sm font-mono truncate">
          {toolName || "unknown"}
        </span>
      </div>
      <div className="shrink-0 w-[250px] text-sm text-muted-foreground truncate">
        {userEmail || "—"}
      </div>
      <div className="shrink-0 w-20 flex justify-end">
        <HookEventBadge eventName={hookEventName} />
      </div>
    </button>
  );
}

function HookEventBadge({ eventName }: { eventName?: string }) {
  const config = useMemo(() => {
    switch (eventName) {
      case "SessionStart":
        return {
          color: "text-blue-500",
          bgColor: "bg-blue-500/10",
          label: "Session Start",
        };
      case "PreToolUse":
        return {
          color: "text-yellow-500",
          bgColor: "bg-yellow-500/10",
          label: "Pre Tool",
        };
      case "PostToolUse":
        return {
          color: "text-emerald-500",
          bgColor: "bg-emerald-500/10",
          label: "Success",
        };
      case "PostToolUseFailure":
        return {
          color: "text-destructive",
          bgColor: "bg-destructive/10",
          label: "Failure",
        };
      default:
        return {
          color: "text-muted-foreground",
          bgColor: "bg-muted",
          label: "Unknown",
        };
    }
  }, [eventName]);

  return (
    <div
      className={cn(
        "inline-flex items-center gap-1.5 px-2 py-1 rounded-md text-xs font-medium",
        config.bgColor,
        config.color,
      )}
    >
      <div
        className={cn(
          "size-1.5 rounded-full",
          config.color === "text-muted-foreground"
            ? "bg-muted-foreground"
            : "bg-current",
        )}
      />
      {config.label}
    </div>
  );
}
