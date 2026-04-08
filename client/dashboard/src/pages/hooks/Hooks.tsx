import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { EnterpriseGate } from "@/components/enterprise-gate";
import { InsightsSidebar } from "@/components/insights-sidebar";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { Page } from "@/components/page-layout";
import { PieProgress } from "@/components/PieProgress";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { MultiSearch } from "@/components/ui/multi-search";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useSlugs } from "@/contexts/Sdk";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { useServerNameMappings } from "@/hooks/useServerNameMappings";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import {
  getPresetRange,
  TimeRangePicker,
  type DateRangePreset,
} from "@gram-ai/elements";
import { telemetryGetHooksSummary } from "@gram/client/funcs/telemetryGetHooksSummary";
import { telemetryListHooksTraces } from "@gram/client/funcs/telemetryListHooksTraces";
import type {
  GetHooksSummaryResult,
  HooksServerSummary,
  HookTraceSummary as HookTrace,
  LogFilter,
  SkillSummary,
  TelemetryLogRecord,
  TypesToInclude,
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
import { EditServerNameDialog } from "./EditServerNameDialog";
import { HooksEmptyState } from "./HooksEmptyState";
import { HookSourceIcon } from "./HookSourceIcon";
import { HooksSetupButton } from "./HooksSetupDialog";

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

// Generic filter chip that displays one thing but filters on multiple values
interface FilterChip {
  // What to display in the UI (e.g., "Linear" or "Tom")
  display: string;
  // The actual filter values to use in queries (e.g., ["claude_ai_Linear", "my_linear_server"] or ["tom@speakeasy.com"])
  filters: string[];
  // The attribute path to filter on (e.g., "gram.tool_call.source" or "user.email")
  path: string;
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

  // Server name mappings for display overrides
  const serverNameMappings = useServerNameMappings();

  const initialServer = searchParams.get("server");
  const initialUserEmail = searchParams.get("user");

  // Parse initial hook types from URL (default to all types)
  const initialHookTypes = searchParams.get("hookTypes");
  const defaultHookTypes: TypesToInclude[] = ["mcp", "local", "skill"];
  const parsedHookTypes: TypesToInclude[] = initialHookTypes
    ? (initialHookTypes
        .split(",")
        .filter((t) =>
          ["mcp", "local", "skill"].includes(t),
        ) as TypesToInclude[])
    : defaultHookTypes;

  // Active filter chips
  const [activeFilters, setActiveFilters] = useState<FilterChip[]>(() => {
    const filters: FilterChip[] = [];

    // Parse comma-separated server filters
    if (initialServer) {
      const serverValues = initialServer
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      serverValues.forEach((value) => {
        filters.push({
          display: value,
          filters: [value],
          path: "gram.tool_call.source",
        });
      });
    }

    // Parse comma-separated user filters
    if (initialUserEmail) {
      const userValues = initialUserEmail
        .split(",")
        .map((s) => s.trim())
        .filter(Boolean);
      userValues.forEach((value) => {
        filters.push({
          display: value,
          filters: [value],
          path: "user.email",
        });
      });
    }

    return filters;
  });

  const [serverInput, setServerInput] = useState("");
  const [userEmailInput, setUserEmailInput] = useState("");
  const [selectedHookTypes, setSelectedHookTypes] =
    useState<TypesToInclude[]>(parsedHookTypes);
  const [summaryView, setSummaryView] = useState<
    "servers" | "users" | "skills"
  >("servers");
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

  // Build attribute filters from active filter chips
  const logFilters = useMemo(() => {
    const filters: LogFilter[] = [];

    // Each chip becomes its own filter entry to preserve operator semantics:
    // - Single-value chips (from search input) use "contains" (substring match)
    // - Multi-value chips (from table clicks with grouped servers) use "in" (exact match)
    for (const chip of activeFilters) {
      filters.push({
        path: chip.path,
        operator: chip.filters.length > 1 ? "in" : "contains",
        values: chip.filters,
      });
    }

    return filters.length > 0 ? filters : undefined;
  }, [activeFilters]);

  // Fetch hooks traces with infinite scroll (pre-aggregated by backend)
  const {
    data: tracesData,
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
        "hooks-traces",
        activeFilters,
        selectedHookTypes,
        from.toISOString(),
        to.toISOString(),
      ],
      queryFn: ({ pageParam }) =>
        unwrapAsync(
          telemetryListHooksTraces(client, {
            listHooksTracesPayload: {
              from,
              to,
              filters: logFilters,
              typesToInclude:
                selectedHookTypes.length > 0 ? selectedHookTypes : undefined,
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

  // No more client-side grouping - traces are pre-aggregated by backend
  const groupedTraces = useMemo(() => {
    return tracesData?.pages.flatMap((page) => page.traces) ?? [];
  }, [tracesData]);

  // Add a filter chip
  const addFilter = useCallback(
    (chip: FilterChip) => {
      setActiveFilters((prev) => {
        // Check if this exact filter already exists
        const exists = prev.some(
          (f) => f.path === chip.path && f.display === chip.display,
        );
        if (exists) return prev;

        // Add the new filter alongside existing ones
        const newFilters = [...prev, chip];

        // Update URL params with all values (comma-separated)
        setSearchParams(
          (urlPrev) => {
            const next = new URLSearchParams(urlPrev);
            if (chip.path === "gram.tool_call.source") {
              const serverFilters = newFilters
                .filter((f) => f.path === "gram.tool_call.source")
                .map((f) => f.display);
              next.set("server", serverFilters.join(","));
            } else if (chip.path === "user.email") {
              const userFilters = newFilters
                .filter((f) => f.path === "user.email")
                .map((f) => f.display);
              next.set("user", userFilters.join(","));
            }
            return next;
          },
          { replace: true },
        );

        return newFilters;
      });
    },
    [setSearchParams],
  );

  // Remove a filter chip by path and display value
  const removeFilter = useCallback(
    (path: string, display?: string) => {
      setActiveFilters((prev) => {
        const newFilters = display
          ? prev.filter((f) => !(f.path === path && f.display === display))
          : prev.filter((f) => f.path !== path);

        // Update URL params with remaining values
        setSearchParams(
          (urlPrev) => {
            const next = new URLSearchParams(urlPrev);
            if (path === "gram.tool_call.source") {
              const serverFilters = newFilters
                .filter((f) => f.path === "gram.tool_call.source")
                .map((f) => f.display);
              if (serverFilters.length > 0) {
                next.set("server", serverFilters.join(","));
              } else {
                next.delete("server");
              }
            } else if (path === "user.email") {
              const userFilters = newFilters
                .filter((f) => f.path === "user.email")
                .map((f) => f.display);
              if (userFilters.length > 0) {
                next.set("user", userFilters.join(","));
              } else {
                next.delete("user");
              }
            }
            return next;
          },
          { replace: true },
        );

        return newFilters;
      });
    },
    [setSearchParams],
  );

  // Debounced server filter from search input
  useEffect(() => {
    if (!serverInput.trim()) return;

    const timeoutId = setTimeout(() => {
      addFilter({
        display: serverInput,
        filters: [serverInput],
        path: "gram.tool_call.source",
      });
      setServerInput(""); // Clear input after adding
    }, 500);
    return () => clearTimeout(timeoutId);
  }, [serverInput, addFilter]);

  // Debounced user filter from search input
  useEffect(() => {
    if (!userEmailInput.trim()) return;

    const timeoutId = setTimeout(() => {
      addFilter({
        display: userEmailInput,
        filters: [userEmailInput],
        path: "user.email",
      });
      setUserEmailInput(""); // Clear input after adding
    }, 500);
    return () => clearTimeout(timeoutId);
  }, [userEmailInput, addFilter]);

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

  const handleHookTypesChange = useCallback(
    (types: TypesToInclude[]) => {
      setSelectedHookTypes(types);
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (types.length === 3) {
            // All types selected - remove param (default)
            next.delete("hookTypes");
          } else if (types.length > 0) {
            next.set("hookTypes", types.join(","));
          } else {
            // No types selected - still store it
            next.set("hookTypes", "");
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
          {isLogsDisabled ? (
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
          ) : (
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
                  userEmailInput={userEmailInput}
                  setUserEmailInput={setUserEmailInput}
                  activeFilters={activeFilters}
                  addFilter={addFilter}
                  removeFilter={removeFilter}
                  selectedHookTypes={selectedHookTypes}
                  onHookTypesChange={handleHookTypesChange}
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
                  serverNameMappings={serverNameMappings}
                />
              </EnterpriseGate>
            </Page.Body>
          )}
        </Page>
      </div>
    </InsightsSidebar>
  );
}

function HooksInnerContent({
  isLoading,
  isFetching,
  error,
  summaryData,
  summaryView,
  onSummaryViewChange,
  groupedTraces,
  serverInput,
  setServerInput,
  userEmailInput,
  setUserEmailInput,
  activeFilters,
  addFilter,
  removeFilter,
  selectedHookTypes,
  onHookTypesChange,
  expandedTraceId,
  toggleExpand,
  selectedLog,
  handleLogClick,
  setSelectedLog,
  containerRef,
  handleScroll,
  hasNextPage,
  isFetchingNextPage,
  dateRange,
  customRange,
  customRangeLabel,
  onDateRangeChange,
  onCustomRangeChange,
  onClearCustomRange,
  projectSlug,
  serverNameMappings,
}: {
  isLogsDisabled: boolean;
  isLoading: boolean;
  isFetching: boolean;
  error: Error | null;
  summaryData?: GetHooksSummaryResult;
  summaryView: "servers" | "users" | "skills";
  onSummaryViewChange: (view: "servers" | "users" | "skills") => void;
  groupedTraces: HookTrace[];
  serverInput: string;
  setServerInput: (value: string) => void;
  userEmailInput: string;
  setUserEmailInput: (value: string) => void;
  activeFilters: FilterChip[];
  addFilter: (chip: FilterChip) => void;
  removeFilter: (path: string, display?: string) => void;
  selectedHookTypes: TypesToInclude[];
  onHookTypesChange: (types: TypesToInclude[]) => void;
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
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
}) {
  const orgRoutes = useOrgRoutes();

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
            <div className="flex items-center gap-2">
              <HooksSetupButton />
              <Button variant="outline" size="sm" asChild>
                <Link to={orgRoutes.logs.href()}>
                  <Settings className="h-4 w-4" />
                  Configure settings
                </Link>
              </Button>
            </div>
          </div>

          {/* Summary Tables */}
          {summaryData &&
            (summaryData.servers.length > 0 ||
              (summaryData.users && summaryData.users.length > 0) ||
              (summaryData.skills && summaryData.skills.length > 0)) && (
              <div className="mb-4 border rounded-lg overflow-hidden">
                {summaryData.servers.length > 0 && (
                  <div className={summaryView !== "servers" ? "hidden" : ""}>
                    <HooksServerTable
                      servers={summaryData.servers}
                      onFilterSelect={addFilter}
                      summaryView={summaryView}
                      onSummaryViewChange={onSummaryViewChange}
                      serverNameMappings={serverNameMappings}
                    />
                  </div>
                )}
                {summaryData.users && summaryData.users.length > 0 && (
                  <div className={summaryView !== "users" ? "hidden" : ""}>
                    <HooksUserTable
                      users={summaryData.users}
                      onFilterSelect={addFilter}
                      summaryView={summaryView}
                      onSummaryViewChange={onSummaryViewChange}
                    />
                  </div>
                )}
                {summaryData.skills && summaryData.skills.length > 0 && (
                  <div className={summaryView !== "skills" ? "hidden" : ""}>
                    <HooksSkillTable
                      skills={summaryData.skills}
                      summaryView={summaryView}
                      onSummaryViewChange={onSummaryViewChange}
                    />
                  </div>
                )}
              </div>
            )}

          {/* Filter and Search Row */}
          <div className="flex items-center gap-2 flex-wrap mt-4">
            <MultiSearch
              value={serverInput}
              onChange={setServerInput}
              placeholder="Filter by server name"
              className="flex-1 min-w-[200px]"
              chips={activeFilters
                .filter((f) => f.path === "gram.tool_call.source")
                .map((f) => ({ display: f.display, value: f.display }))}
              onRemoveChip={(display) =>
                removeFilter("gram.tool_call.source", display)
              }
            />
            <MultiSearch
              value={userEmailInput}
              onChange={setUserEmailInput}
              placeholder="Filter by user email"
              className="flex-1 min-w-[200px]"
              chips={activeFilters
                .filter((f) => f.path === "user.email")
                .map((f) => ({ display: f.display, value: f.display }))}
              onRemoveChip={(display) => removeFilter("user.email", display)}
            />
            <HookTypeFilter
              selectedHookTypes={selectedHookTypes}
              onHookTypesChange={onHookTypesChange}
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
                activeFilters={activeFilters}
                expandedTraceId={expandedTraceId}
                isFetchingNextPage={isFetchingNextPage}
                onToggleExpand={toggleExpand}
                onLogClick={handleLogClick}
                serverNameMappings={serverNameMappings}
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

const HOOK_TYPE_OPTIONS = [
  { label: "MCP Servers", value: "mcp" as TypesToInclude },
  { label: "Local Tools", value: "local" as TypesToInclude },
  { label: "Skills", value: "skill" as TypesToInclude },
];

function HookTypeFilter({
  selectedHookTypes,
  onHookTypesChange,
}: {
  selectedHookTypes: TypesToInclude[];
  onHookTypesChange: (types: TypesToInclude[]) => void;
}) {
  const getButtonText = () => {
    if (selectedHookTypes.length === 3) {
      return "Showing all types";
    }

    if (selectedHookTypes.length === 0) {
      return "No types selected";
    }

    if (selectedHookTypes.length === 1) {
      const selected = HOOK_TYPE_OPTIONS.find(
        (opt) => opt.value === selectedHookTypes[0],
      );
      return `Showing ${selected?.label || selectedHookTypes[0]}`;
    }

    return `Showing ${selectedHookTypes.length} of 3 types`;
  };

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          className="shrink-0 w-[200px] h-[42px] justify-between"
        >
          <span className="text-sm">{getButtonText()}</span>
          <Icon name="chevron-down" className="size-4 ml-2" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[200px] p-3" align="start">
        <div className="space-y-2">
          {HOOK_TYPE_OPTIONS.map((option) => (
            <div key={option.value} className="flex items-center space-x-2">
              <Checkbox
                id={`hook-type-${option.value}`}
                checked={selectedHookTypes.includes(option.value)}
                onCheckedChange={(checked) => {
                  if (checked) {
                    onHookTypesChange([...selectedHookTypes, option.value]);
                  } else {
                    onHookTypesChange(
                      selectedHookTypes.filter((t) => t !== option.value),
                    );
                  }
                }}
              />
              <label
                htmlFor={`hook-type-${option.value}`}
                className="text-sm font-medium leading-none cursor-pointer"
              >
                {option.label}
              </label>
            </div>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
}

interface SummaryItemData {
  name: string;
  displayName?: string;
  toolCallCount: number;
  uniqueTools: number;
  failureRate: number;
}

interface SummaryTableProps {
  items: SummaryItemData[];
  onItemSelect: (key: string) => void;
  onItemEdit?: (key: string) => void;
  sortItems?: (items: SummaryItemData[]) => SummaryItemData[];
  tabValue?: string;
  onTabChange?: (value: string) => void;
  tabs?: Array<{ value: string; label: string }>;
  uniqueToolsLabel?: string;
  toolCallsLabel?: string;
  hideFilterAction?: boolean;
}

function SummaryTable({
  items,
  onItemSelect,
  onItemEdit,
  sortItems,
  tabValue,
  onTabChange,
  tabs,
  uniqueToolsLabel = "Unique Tools",
  toolCallsLabel = "Tool Calls",
  hideFilterAction = false,
}: SummaryTableProps) {
  const [isExpanded, setIsExpanded] = useState(false);
  const sortedItems = useMemo(() => {
    return sortItems ? sortItems(items) : items;
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
        <div className="shrink-0 w-[100px] text-right">{uniqueToolsLabel}</div>
        <div className="shrink-0 w-[100px] text-right">{toolCallsLabel}</div>
        <div className="shrink-0 w-[100px] text-right">Success Rate</div>
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
            className="group w-full flex items-center gap-3 px-5 py-3 border-b last:border-b-0"
          >
            {/* Name + Actions */}
            <div className="flex items-center gap-2 flex-1 min-w-0">
              <span className="text-sm font-medium truncate">
                {item.displayName || item.name}
              </span>

              {/* Actions - shown on hover */}
              <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
                {!hideFilterAction && (
                  <button
                    onClick={() => onItemSelect(item.name)}
                    className="p-1.5 rounded hover:bg-primary/10"
                    title={`Filter by ${item.displayName || item.name}`}
                  >
                    <Filter className="size-4 text-muted-foreground hover:text-primary" />
                  </button>
                )}
                {onItemEdit && (
                  <button
                    onClick={() => onItemEdit(item.name)}
                    className="p-1.5 rounded hover:bg-primary/10"
                    title={`Edit display name for ${item.displayName || item.name}`}
                  >
                    <Icon
                      name="pencil"
                      className="size-4 text-muted-foreground hover:text-primary"
                    />
                  </button>
                )}
              </div>
            </div>

            {/* Unique Tools */}
            <div className="shrink-0 w-[100px] text-right text-sm text-muted-foreground">
              {item.uniqueTools}
            </div>

            {/* Tool Calls (successCount + failureCount (eventCount is something else)) */}
            <div className="shrink-0 w-[100px] text-right text-sm">
              {item.toolCallCount}
            </div>

            {/* Success Rate */}
            <div className="shrink-0 w-[100px] flex justify-end items-center gap-1.5">
              <span className="text-sm font-medium tabular-nums">
                {Math.round((1 - item.failureRate) * 100)}%
              </span>
              <PieProgress
                value={Math.round((1 - item.failureRate) * 100)}
                size={16}
              />
            </div>
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
  onFilterSelect,
  summaryView,
  onSummaryViewChange,
  serverNameMappings,
}: {
  servers: HooksServerSummary[];
  onFilterSelect: (chip: FilterChip) => void;
  summaryView: "servers" | "users" | "skills";
  onSummaryViewChange: (view: "servers" | "users" | "skills") => void;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
}) {
  const [editingServer, setEditingServer] = useState<string | null>(null);

  // Group servers by their final display name and merge metrics
  const items: SummaryItemData[] = useMemo(() => {
    const grouped = new Map<
      string,
      {
        rawNames: string[];
        toolCallCount: number;
        uniqueToolsSet: Set<string>;
        successCount: number;
        failureCount: number;
      }
    >();

    for (const s of servers) {
      const rawName = s.serverName;
      const displayName = !rawName
        ? (serverNameMappings.rawToDisplay.get("") ?? "Local Tools")
        : (serverNameMappings.rawToDisplay.get(rawName) ?? rawName);

      const existing = grouped.get(displayName);
      if (existing) {
        // Merge with existing
        existing.rawNames.push(rawName);
        existing.toolCallCount += s.successCount + s.failureCount;
        existing.successCount += s.successCount;
        existing.failureCount += s.failureCount;
        // Note: uniqueTools from backend are already counted per server
        // We can't accurately merge unique tools across servers, so we sum them
        // This is an approximation - ideally backend would provide this
      } else {
        // Create new entry
        grouped.set(displayName, {
          rawNames: [rawName],
          toolCallCount: s.successCount + s.failureCount,
          uniqueToolsSet: new Set(), // Not used in current logic
          successCount: s.successCount,
          failureCount: s.failureCount,
        });
      }
    }

    // Convert to SummaryItemData array
    return Array.from(grouped.entries()).map(([displayName, data]) => {
      const failureRate =
        data.toolCallCount > 0 ? data.failureCount / data.toolCallCount : 0;

      // For uniqueTools, sum across servers (approximation)
      const uniqueTools = data.rawNames.reduce((sum, rawName) => {
        const server = servers.find((s) => s.serverName === rawName);
        return sum + (server?.uniqueTools || 0);
      }, 0);

      return {
        name: data.rawNames[0], // Use first raw name for filtering
        displayName,
        toolCallCount: data.toolCallCount,
        uniqueTools,
        failureRate,
      };
    });
  }, [servers, serverNameMappings.rawToDisplay]);

  const handleEditClick = (serverName: string) => {
    setEditingServer(serverName);
  };

  const editingServerInfo = useMemo(() => {
    if (editingServer === null) return { overrides: [], unmappedRawName: null };

    // editingServer is a raw server name — find the display name for it
    const displayName =
      serverNameMappings.rawToDisplay.get(editingServer) ?? editingServer;

    // Get all overrides that share this display name (for grouped servers)
    const overridesForDisplay =
      serverNameMappings.displayToOverrides.get(displayName) || [];

    // Check if editingServer itself exists as a raw server name in the servers list
    // (meaning it's unmapped and shows as itself)
    const serverExistsAsRaw = servers.some(
      (s) => s.serverName === editingServer,
    );

    if (serverExistsAsRaw) {
      // Check if this raw server already has an override
      const hasOverride = overridesForDisplay.some(
        (o) => o.rawServerName === editingServer,
      );

      if (!hasOverride && overridesForDisplay.length > 0) {
        // This server exists unmapped alongside other servers that map to it
        return {
          overrides: overridesForDisplay,
          unmappedRawName: editingServer,
        };
      }
    }

    return { overrides: overridesForDisplay, unmappedRawName: null };
  }, [editingServer, serverNameMappings, servers]);

  const handleItemSelect = useCallback(
    (itemName: string) => {
      // Find the item to get its display name and raw server names
      const item = items.find((i) => i.name === itemName);
      if (!item) return;

      const displayName = item.displayName || item.name;

      // Get all raw server names that have overrides pointing to this display name
      const mappedRawNames =
        serverNameMappings.displayToRaws.get(displayName) || [];

      // Also include the display name itself if it exists as a raw server (no override to it)
      const allRawNames = new Set(mappedRawNames);

      // Always include the item's actual raw name (e.g. "" for Local Tools)
      allRawNames.add(item.name);

      // Check if the display name itself exists as a raw server name in the data
      // (this handles the case where "B" is both a display name and a raw server)
      const displayNameExistsAsRaw = servers.some(
        (s) => s.serverName === displayName,
      );
      if (displayNameExistsAsRaw) {
        allRawNames.add(displayName);
      }

      // Create a filter chip
      onFilterSelect({
        display: displayName,
        filters: Array.from(allRawNames),
        path: "gram.tool_call.source",
      });
    },
    [items, onFilterSelect, serverNameMappings.displayToRaws, servers],
  );

  return (
    <>
      <SummaryTable
        items={items}
        onItemSelect={handleItemSelect}
        onItemEdit={handleEditClick}
        sortItems={(items) =>
          [...items].sort((a, b) => {
            // Sort by tool call count descending
            return b.toolCallCount - a.toolCallCount;
          })
        }
        tabValue={summaryView}
        onTabChange={(v) =>
          onSummaryViewChange(v as "servers" | "users" | "skills")
        }
        tabs={[
          { value: "servers", label: "Servers" },
          { value: "users", label: "Users" },
          { value: "skills", label: "Skills" },
        ]}
      />

      <EditServerNameDialog
        key={editingServer}
        open={editingServer !== null}
        onOpenChange={(open) => !open && setEditingServer(null)}
        serverName={editingServer ?? ""}
        groupedOverrides={editingServerInfo.overrides}
        unmappedRawName={editingServerInfo.unmappedRawName}
        upsert={serverNameMappings.upsert}
        remove={serverNameMappings.remove}
        isUpserting={serverNameMappings.isUpserting}
        isDeleting={serverNameMappings.isDeleting}
      />
    </>
  );
}

function HooksUserTable({
  users,
  onFilterSelect,
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
  onFilterSelect: (chip: FilterChip) => void;
  summaryView: "servers" | "users" | "skills";
  onSummaryViewChange: (view: "servers" | "users" | "skills") => void;
}) {
  const items: SummaryItemData[] = users.map((u) => ({
    name: u.userEmail,
    displayName:
      u.userEmail === "Unknown" || u.userEmail === ""
        ? "Unknown user"
        : u.userEmail,
    toolCallCount: u.successCount + u.failureCount,
    uniqueTools: u.uniqueTools,
    failureRate: u.failureRate,
  }));

  const handleItemSelect = useCallback(
    (itemName: string) => {
      const item = items.find((i) => i.name === itemName);
      if (!item) return;

      onFilterSelect({
        display: item.displayName || item.name,
        filters: [item.name], // For users, filter by the actual email
        path: "user.email",
      });
    },
    [items, onFilterSelect],
  );

  return (
    <SummaryTable
      items={items}
      onItemSelect={handleItemSelect}
      sortItems={(items) =>
        [...items].sort((a, b) => {
          return b.toolCallCount - a.toolCallCount;
        })
      }
      tabValue={summaryView}
      onTabChange={(v) =>
        onSummaryViewChange(v as "servers" | "users" | "skills")
      }
      tabs={[
        { value: "servers", label: "Servers" },
        { value: "users", label: "Users" },
        { value: "skills", label: "Skills" },
      ]}
    />
  );
}

function HooksSkillTable({
  skills,
  summaryView,
  onSummaryViewChange,
}: {
  skills: SkillSummary[];
  summaryView: "servers" | "users" | "skills";
  onSummaryViewChange: (view: "servers" | "users" | "skills") => void;
}) {
  const items: SummaryItemData[] = skills.map((s) => ({
    name: s.skillName,
    displayName: s.skillName,
    toolCallCount: s.useCount,
    uniqueTools: s.uniqueUsers,
    failureRate: 0, // Skills don't have failure rates in the summary
  }));

  return (
    <SummaryTable
      items={items}
      onItemSelect={() => {
        // Skills don't have filtering yet
      }}
      sortItems={(items) =>
        [...items].sort((a, b) => {
          return b.toolCallCount - a.toolCallCount;
        })
      }
      tabValue={summaryView}
      onTabChange={(v) =>
        onSummaryViewChange(v as "servers" | "users" | "skills")
      }
      tabs={[
        { value: "servers", label: "Servers" },
        { value: "users", label: "Users" },
        { value: "skills", label: "Skills" },
      ]}
      uniqueToolsLabel="Unique Users"
      toolCallsLabel="Invocations"
      hideFilterAction
    />
  );
}

function HooksTraceContent({
  error,
  isLoading,
  groupedTraces,
  activeFilters,
  expandedTraceId,
  isFetchingNextPage,
  onToggleExpand,
  onLogClick,
  serverNameMappings,
}: {
  error: Error | null;
  isLoading: boolean;
  groupedTraces: HookTrace[];
  activeFilters: FilterChip[];
  expandedTraceId: string | null;
  isFetchingNextPage: boolean;
  onToggleExpand: (traceId: string) => void;
  onLogClick: (log: TelemetryLogRecord) => void;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
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
    const hasFilters = activeFilters.length > 0;

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
          serverNameMappings={serverNameMappings}
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
  serverNameMappings,
}: {
  trace: HookTrace;
  isExpanded: boolean;
  onToggle: () => void;
  onLogClick: (log: TelemetryLogRecord) => void;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
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
  const skillName = trace.skillName;
  const userEmail = trace.userEmail;
  const hookSource = trace.hookSource;

  // Apply display name mapping
  const displayServerName = useMemo(() => {
    if (!serverName) return serverNameMappings.rawToDisplay.get("") ?? null;
    return serverNameMappings.rawToDisplay.get(serverName) ?? serverName;
  }, [serverName, serverNameMappings.rawToDisplay]);

  const serverNameBadge = useMemo(() => {
    // For skills, show [Skill] badge
    if (toolName === "Skill" && skillName) {
      return (
        <span className="text-xs font-mono truncate px-2 py-1 rounded-md shrink-0 bg-purple-500/10 text-purple-600 dark:text-purple-400 border border-purple-500/20 font-medium">
          Skill
        </span>
      );
    }

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
        {displayServerName || "local"}
      </span>
    );
  }, [displayServerName, toolName, skillName]);

  const statusConfig = useMemo(() => {
    if (trace.hookStatus === "failure") {
      return {
        color: "text-destructive",
        bgColor: "bg-destructive/10",
        label: "Failure",
      };
    } else if (trace.hookStatus === "success") {
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
  }, [trace.hookStatus]);

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
            {toolName === "Skill" && skillName
              ? skillName
              : toolName || "unknown"}
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
