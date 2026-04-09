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
import { MetricCard } from "@/components/chart/MetricCard";
import { formatChartLabel } from "@/components/chart/chartUtils";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import {
  BarElement,
  BarController,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
  Chart as ChartJS,
  type TooltipItem,
  type ChartOptions,
} from "chart.js";
import { Bar, Line } from "react-chartjs-2";
import { Filter, Settings } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { LogDetailSheet } from "../logs/LogDetailSheet";
import { TraceLogsList } from "../logs/TraceLogsList";
import { EditServerNameDialog } from "./EditServerNameDialog";
import { HooksEmptyState } from "./HooksEmptyState";
import { HookSourceIcon } from "./HookSourceIcon";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  BarController,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
);

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
      <div className="flex h-full flex-col overflow-hidden">
        <Page>
          <Page.Header>
            <Page.Header.Breadcrumbs fullWidth />
          </Page.Header>
          {isLogsDisabled ? (
            <Page.Body fullWidth className="space-y-6">
              <div className="flex min-w-0 flex-col gap-1">
                <h1 className="text-xl font-semibold">Hooks</h1>
                <p className="text-muted-foreground text-sm">
                  Monitor hook events and tool executions across all servers
                </p>
              </div>
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
  const { from, to } = useMemo(
    () => customRange ?? getPresetRange(dateRange),
    [customRange, dateRange],
  );

  return (
    <>
      <div className="flex min-h-0 w-full flex-1 flex-col">
        {/* Header section */}
        <div className="flex shrink-0 flex-col gap-6 px-8 pt-8 pb-4">
          <div className="flex items-start justify-between gap-4">
            <div className="flex min-w-0 flex-col gap-1">
              <h1 className="text-xl font-semibold">Hooks</h1>
              <p className="text-muted-foreground text-sm">
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

          {/* Filter and Search Row */}
          <div className="flex flex-wrap items-center gap-2">
            <MultiSearch
              value={serverInput}
              onChange={setServerInput}
              placeholder="Filter by server name"
              className="min-w-[200px] flex-1"
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
              className="min-w-[200px] flex-1"
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

          <HooksAnalytics
            groupedTraces={groupedTraces}
            serverNameMappings={serverNameMappings}
            from={from}
            to={to}
          />

          {/* Summary Tables */}
          {summaryData &&
            (summaryData.servers.length > 0 ||
              (summaryData.users && summaryData.users.length > 0) ||
              (summaryData.skills && summaryData.skills.length > 0)) && (
              <div className="overflow-hidden rounded-lg border">
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
        </div>

        {/* Content section */}
        <div className="min-h-0 flex-1 overflow-hidden border-t">
          <div className="bg-background flex h-full flex-col">
            {isFetching && groupedTraces.length > 0 && (
              <div className="bg-primary/20 absolute top-0 right-0 left-0 z-20 h-1">
                <div className="bg-primary h-full animate-pulse" />
              </div>
            )}

            {/* Header */}
            <div className="bg-muted/30 text-muted-foreground flex shrink-0 items-center gap-3 border-b px-5 py-2.5 text-xs font-medium tracking-wide uppercase">
              <div className="w-[150px] shrink-0">Timestamp</div>
              <div className="w-5 shrink-0" />
              <div className="min-w-0 flex-1">Server / Tool</div>
              <div className="w-[260px] shrink-0">User</div>
              <div className="w-[120px] shrink-0">Source</div>
              <div className="w-20 shrink-0 text-right">Status</div>
            </div>

            {/* Scrollable trace list */}
            <div
              ref={containerRef}
              className="flex-1 overflow-y-auto"
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
              <div className="bg-muted/30 text-muted-foreground flex shrink-0 items-center gap-4 border-t px-5 py-3 text-sm">
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
          className="h-[42px] w-[200px] shrink-0 justify-between"
        >
          <span className="text-sm">{getButtonText()}</span>
          <Icon name="chevron-down" className="ml-2 size-4" />
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
                className="cursor-pointer text-sm leading-none font-medium"
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
      <div className="bg-muted/30 text-muted-foreground flex items-center gap-3 border-b px-5 py-1 text-xs font-medium tracking-wide uppercase">
        {/* First column - tabs or header */}
        <div className="min-w-0 flex-1">
          {tabs && tabValue && onTabChange ? (
            <Tabs value={tabValue} onValueChange={onTabChange}>
              <TabsList className="h-7 p-0.5">
                {tabs.map((tab) => (
                  <TabsTrigger
                    key={tab.value}
                    value={tab.value}
                    className="h-6 px-2.5 text-xs"
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
        <div className="w-[100px] shrink-0 text-right">{uniqueToolsLabel}</div>
        <div className="w-[100px] shrink-0 text-right">{toolCallsLabel}</div>
        <div className="w-[100px] shrink-0 text-right">Success Rate</div>
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
            className="group flex w-full items-center gap-3 border-b px-5 py-3 last:border-b-0"
          >
            {/* Name + Actions */}
            <div className="flex min-w-0 flex-1 items-center gap-2">
              <span className="truncate text-sm font-medium">
                {item.displayName || item.name}
              </span>

              {/* Actions - shown on hover */}
              <div className="flex shrink-0 items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                {!hideFilterAction && (
                  <button
                    onClick={() => onItemSelect(item.name)}
                    className="hover:bg-primary/10 rounded p-1.5"
                    title={`Filter by ${item.displayName || item.name}`}
                  >
                    <Filter className="text-muted-foreground hover:text-primary size-4" />
                  </button>
                )}
                {onItemEdit && (
                  <button
                    onClick={() => onItemEdit(item.name)}
                    className="hover:bg-primary/10 rounded p-1.5"
                    title={`Edit display name for ${item.displayName || item.name}`}
                  >
                    <Icon
                      name="pencil"
                      className="text-muted-foreground hover:text-primary size-4"
                    />
                  </button>
                )}
              </div>
            </div>

            {/* Unique Tools */}
            <div className="text-muted-foreground w-[100px] shrink-0 text-right text-sm">
              {item.uniqueTools}
            </div>

            {/* Tool Calls (successCount + failureCount (eventCount is something else)) */}
            <div className="w-[100px] shrink-0 text-right text-sm">
              {item.toolCallCount}
            </div>

            {/* Success Rate */}
            <div className="flex w-[100px] shrink-0 items-center justify-end gap-1.5">
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
          className="bg-background/95 border-border/50 hover:bg-muted absolute bottom-2 left-1/2 h-7 -translate-x-1/2 border px-2 shadow-sm backdrop-blur-sm"
        >
          <Icon
            name={isExpanded ? "chevrons-up" : "chevrons-down"}
            className="size-3.5"
          />
          <span className="ml-1 text-xs">
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
        <div className="bg-destructive/10 flex size-12 items-center justify-center rounded-full">
          <Icon name="x" className="text-destructive size-6" />
        </div>
        <span className="text-foreground font-medium">
          Error loading hook events
        </span>
        <span className="text-muted-foreground max-w-sm text-center text-sm">
          {error.message}
        </span>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
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
          <div className="bg-muted flex size-12 items-center justify-center rounded-full">
            <Icon name="inbox" className="text-muted-foreground size-6" />
          </div>
          <span className="text-foreground font-medium">
            No matching hook events
          </span>
          <span className="text-muted-foreground max-w-sm text-sm">
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
        <div className="text-muted-foreground flex items-center justify-center gap-2 border-t py-4">
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
        <span className="shrink-0 truncate rounded-md border border-purple-500/20 bg-purple-500/10 px-2 py-1 font-mono text-xs font-medium text-purple-600 dark:text-purple-400">
          Skill
        </span>
      );
    }

    const isLocal = !serverName;
    return (
      <span
        className={cn(
          "shrink-0 truncate rounded-md px-2 py-1 font-mono text-xs",
          isLocal
            ? "bg-muted/50 text-muted-foreground"
            : "bg-primary/10 text-primary border-primary/20 border font-medium",
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
    <div className="border-border/50 border-b last:border-b-0">
      {/* Parent trace row */}
      <button
        onClick={onToggle}
        className="hover:bg-muted/50 flex w-full items-center gap-3 px-5 py-2.5 text-left transition-colors"
      >
        {/* Timestamp */}
        <div className="text-muted-foreground w-[150px] shrink-0 font-mono text-sm">
          {timeAgo}
        </div>

        {/* Expand/collapse indicator */}
        <div className="flex w-5 shrink-0 items-center justify-center">
          <Icon
            name={isExpanded ? "chevron-down" : "chevron-right"}
            className="text-muted-foreground size-4"
          />
        </div>

        {/* Server badge + Tool name */}
        <div className="flex min-w-0 flex-1 items-center gap-2">
          {serverNameBadge}
          <span className="truncate font-mono text-sm">
            {toolName === "Skill" && skillName
              ? skillName
              : toolName || "unknown"}
          </span>
        </div>

        {/* User email */}
        <div className="text-muted-foreground w-[260px] shrink-0 truncate text-sm">
          {userEmail || "—"}
        </div>

        {/* Hook source */}
        <div className="flex w-[120px] shrink-0 items-center gap-2">
          <HookSourceIcon source={hookSource} className="size-4 shrink-0" />
          {hookSource && (
            <span className="text-foreground truncate text-xs font-medium">
              {hookSource}
            </span>
          )}
        </div>

        {/* Status badge */}
        <div className="flex w-20 shrink-0 justify-end">
          <div
            className={cn(
              "inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium",
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

function successRateColor(rate: number): string {
  if (rate >= 90) return "#10b981";
  if (rate >= 70) return "#f59e0b";
  return "#ef4444";
}

function successRateClass(rate: number): string {
  if (rate >= 90) return "text-emerald-600";
  if (rate >= 70) return "text-amber-500";
  return "text-red-500";
}

function ServerActivityChart({
  servers,
  serverNameMappings,
}: {
  servers: HooksServerSummary[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
}) {
  const items = useMemo(
    () =>
      servers
        .map((s) => ({
          key: !s.serverName
            ? (serverNameMappings.rawToDisplay.get("") ?? "Local Tools")
            : (serverNameMappings.rawToDisplay.get(s.serverName) ??
              s.serverName),
          value: s.successCount + s.failureCount,
          successRate: (1 - s.failureRate) * 100,
        }))
        .sort((a, b) => b.value - a.value),
    [servers, serverNameMappings.rawToDisplay],
  );

  return (
    <BarList
      items={items}
      barClassName="bg-blue-500"
      renderLabel={(item) => <span className="truncate">{item.key}</span>}
      renderRight={(item) => (
        <span
          className={cn(
            "w-10 text-right text-xs font-medium tabular-nums",
            successRateClass(item.successRate),
          )}
        >
          {item.successRate.toFixed(0)}%
        </span>
      )}
    />
  );
}

function SourceVolumeChart({ traces }: { traces: HookTrace[] }) {
  const items = useMemo(() => {
    const map = new Map<string, { total: number; success: number }>();
    for (const t of traces) {
      const source = t.hookSource ?? "unknown";
      const entry = map.get(source) ?? { total: 0, success: 0 };
      entry.total += 1;
      if (t.hookStatus === "success") entry.success += 1;
      map.set(source, entry);
    }
    return Array.from(map.entries())
      .map(([source, { total, success }]) => ({
        key: source,
        value: total,
        successRate: total > 0 ? (success / total) * 100 : 0,
      }))
      .sort((a, b) => {
        if (a.key === "unknown") return 1;
        if (b.key === "unknown") return -1;
        return b.value - a.value;
      });
  }, [traces]);

  return (
    <BarList
      items={items}
      barClassName="bg-[hsl(280,40%,50%)]"
      renderLabel={(item) => (
        <>
          <span className="truncate">{item.key}</span>
          <HookSourceIcon
            source={item.key}
            className="text-muted-foreground size-3.5 shrink-0"
          />
        </>
      )}
      renderRight={(item) => (
        <span
          className={cn(
            "w-10 text-right text-xs font-medium tabular-nums",
            successRateClass(item.successRate),
          )}
        >
          {item.successRate.toFixed(0)}%
        </span>
      )}
    />
  );
}

// Shared ranked bar list used by volume/error breakdown charts
function BarList<T extends { key: string; value: number }>({
  items,
  maxVisible = 5,
  barClassName = "bg-primary",
  renderLabel,
  renderRight,
}: {
  items: T[];
  maxVisible?: number;
  barClassName?: string;
  renderLabel: (item: T) => React.ReactNode;
  renderRight?: (item: T) => React.ReactNode;
}) {
  const [expanded, setExpanded] = useState(false);
  const maxValue = items[0]?.value ?? 1;
  const visible = expanded ? items : items.slice(0, maxVisible);

  if (items.length === 0) {
    return (
      <div className="text-muted-foreground flex h-16 items-center justify-center text-sm">
        No data
      </div>
    );
  }

  return (
    <div className="space-y-2.5">
      {visible.map((item) => (
        <div key={item.key} className="flex min-w-0 items-center gap-3">
          <div className="text-muted-foreground flex w-28 min-w-0 shrink-0 items-center justify-end gap-1.5 text-xs">
            {renderLabel(item)}
          </div>
          <div className="bg-muted h-1.5 flex-1 overflow-hidden rounded-full">
            <div
              className={cn("h-full rounded-full", barClassName)}
              style={{
                width: `${Math.max(2, (item.value / maxValue) * 100)}%`,
              }}
            />
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <span className="text-muted-foreground w-8 text-right text-xs tabular-nums">
              {item.value.toLocaleString()}
            </span>
            {renderRight?.(item)}
          </div>
        </div>
      ))}
      {items.length > maxVisible && (
        <button
          onClick={() => setExpanded((v) => !v)}
          className="text-muted-foreground hover:text-foreground mt-1 text-xs transition-colors"
        >
          {expanded ? "Show less" : `+${items.length - maxVisible} more`}
        </button>
      )}
    </div>
  );
}

const CHART_COLORS = {
  label: "#737373", // neutral-500
  labelFaded: "#A3A3A3", // neutral-400
  gridLine: "#e5e5e5", // neutral-200
  tooltipBg: "#171717", // neutral-900
  tooltipTitle: "#fafafa", // neutral-50
  tooltipBody: "#d4d4d4", // neutral-300
  tooltipBorder: "#262626", // neutral-800
} as const;

const USER_SOURCE_COLORS = [
  "#38bdf8", // sky-400
  "#34d399", // emerald-400
  "#a78bfa", // violet-400
  "#fb7185", // rose-400
  "#facc15", // yellow-400
  "#2dd4bf", // teal-400
  "#818cf8", // indigo-400
  "#e879f9", // fuchsia-400
  "#a3e635", // lime-400
];

function UserVolumeList({
  traces,
  handleFilter,
}: {
  traces: HookTrace[];
  handleFilter?: (source: string, userEmail: string) => void;
}) {
  const { labels, datasets } = useMemo(() => {
    // Build userEmail → hookSource → count
    const userMap = new Map<string, Map<string, number>>();
    const sourceSet = new Set<string>();
    for (const t of traces) {
      const user = t.userEmail;
      if (!user) continue;
      const source = t.hookSource ?? "unknown";
      sourceSet.add(source);
      const inner = userMap.get(user) ?? new Map<string, number>();
      inner.set(source, (inner.get(source) ?? 0) + 1);
      userMap.set(user, inner);
    }

    // Sort users by total count desc
    const sortedUsers = Array.from(userMap.entries())
      .map(([email, sourceCounts]) => ({
        email,
        total: Array.from(sourceCounts.values()).reduce((a, b) => a + b, 0),
        sourceCounts,
      }))
      .sort((a, b) => b.total - a.total);

    // Sort sources by total usage desc for consistent legend ordering
    const sortedSources = Array.from(sourceSet).sort((a, b) => {
      const aTotal = sortedUsers.reduce(
        (s, u) => s + (u.sourceCounts.get(a) ?? 0),
        0,
      );
      const bTotal = sortedUsers.reduce(
        (s, u) => s + (u.sourceCounts.get(b) ?? 0),
        0,
      );
      return bTotal - aTotal;
    });

    const chartLabels = sortedUsers.map((u) => u.email);

    const chartDatasets = sortedSources.map((source, i) => ({
      label: source,
      barThickness: 24,
      data: sortedUsers.map((u) => u.sourceCounts.get(source) ?? 0),
      backgroundColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length],
      borderWidth: 0,
    }));

    return { labels: chartLabels, datasets: chartDatasets };
  }, [traces]);

  if (labels.length === 0) return null;

  const barHeight = 24;
  const spacerHeight = 10;
  const legendHeight = 64;
  const containerHeight = Math.max(
    120,
    labels.length * (barHeight + spacerHeight) + legendHeight,
  );

  const stackTotalPlugin = {
    id: "stackTotal",
    afterDatasetsDraw(chart: ChartJS) {
      const { ctx, data } = chart;
      const lastMeta = chart.getDatasetMeta(data.datasets.length - 1);
      ctx.save();
      ctx.font = "12px";
      ctx.fillStyle = CHART_COLORS.label;
      ctx.textAlign = "left";
      ctx.textBaseline = "middle";
      lastMeta.data.forEach((bar, i) => {
        const total = data.datasets.reduce(
          (sum, ds) => sum + ((ds.data[i] as number) || 0),
          0,
        );
        ctx.fillText(String(total), bar.x + 4, bar.y);
      });
      ctx.restore();
    },
  };

  const options: ChartOptions<"bar"> = {
    indexAxis: "y",
    responsive: true,
    onClick(_, elements) {
      if (!elements.length || !handleFilter) return;
      const { datasetIndex, index } = elements[0];
      const source = datasets[datasetIndex]?.label;
      const userEmail = labels[index];
      if (source && userEmail) handleFilter(source, userEmail);
    },
    maintainAspectRatio: false,
    scales: {
      x: {
        stacked: true,
        grid: { color: CHART_COLORS.gridLine },
        ticks: { color: CHART_COLORS.labelFaded, precision: 0 },
        afterFit(scale) {
          scale.paddingRight = 30;
        },
      },
      y: {
        stacked: true,
        ticks: { color: CHART_COLORS.label, crossAlign: "far", padding: 2 },
        grid: { display: false },
      },
    },
    plugins: {
      legend: {
        position: "bottom",
        align: "end",
        labels: {
          color: CHART_COLORS.label,
          boxWidth: 12,
          padding: 8,
        },
      },
      tooltip: {
        backgroundColor: CHART_COLORS.tooltipBg,
        titleColor: CHART_COLORS.tooltipTitle,
        bodyColor: CHART_COLORS.tooltipBody,
        borderColor: CHART_COLORS.tooltipBorder,
        borderWidth: 1,
        callbacks: {
          label: (item: TooltipItem<"bar">) =>
            ` ${item.dataset.label}: ${item.parsed.x}`,
        },
      },
    },
  };

  return (
    <div style={{ height: containerHeight }}>
      <Bar
        plugins={[stackTotalPlugin]}
        data={{ labels, datasets }}
        options={{
          ...options,
          onHover(event, elements) {
            const el = event.native?.target as HTMLElement | null;
            if (el) el.style.cursor = elements.length ? "pointer" : "default";
          },
        }}
      />
    </div>
  );
}

function ServerErrorRateChart({
  servers,
  serverNameMappings,
}: {
  servers: HooksServerSummary[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
}) {
  const items = useMemo(
    () =>
      servers
        .filter((s) => s.failureCount > 0)
        .map((s) => ({
          label: !s.serverName
            ? (serverNameMappings.rawToDisplay.get("") ?? "Local Tools")
            : (serverNameMappings.rawToDisplay.get(s.serverName) ??
              s.serverName),
          errorRate: s.failureRate * 100,
          errorCount: s.failureCount,
          total: s.successCount + s.failureCount,
        }))
        .sort((a, b) => b.errorRate - a.errorRate),
    [servers, serverNameMappings.rawToDisplay],
  );

  const height = Math.max(120, items.length * 28 + 40);

  const chartData = {
    labels: items.map((i) => i.label),
    datasets: [
      {
        data: items.map((i) => i.errorRate),
        backgroundColor: items.map((i) => successRateColor(100 - i.errorRate)),
        borderWidth: 0,
        borderRadius: 3,
        barThickness: 16,
      },
    ],
  };

  const options = {
    indexAxis: "y" as const,
    animation: false as const,
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: { display: false },
      tooltip: {
        backgroundColor: "rgba(0,0,0,0.85)",
        titleColor: "#fff",
        bodyColor: "#e5e7eb",
        borderColor: "rgba(255,255,255,0.1)",
        borderWidth: 1,
        padding: 10,
        callbacks: {
          label: (ctx: TooltipItem<"bar">) => {
            const item = items[ctx.dataIndex];
            return [
              ` Error rate: ${(ctx.parsed.x ?? 0).toFixed(1)}%`,
              ` Errors: ${item?.errorCount.toLocaleString()} of ${item?.total.toLocaleString()}`,
            ];
          },
        },
      },
    },
    scales: {
      x: {
        min: 0,
        max: 100,
        grid: { color: "rgba(128,128,128,0.15)" },
        ticks: {
          color: "#64748b",
          callback: (v: number | string) => `${v}%`,
        },
      },
      y: {
        grid: { display: false },
        ticks: { color: "#94a3b8", font: { size: 12 } },
      },
    },
  };

  if (items.length === 0) {
    return (
      <div className="text-muted-foreground flex h-16 items-center justify-center text-sm">
        No errors in this period
      </div>
    );
  }

  return (
    <div style={{ position: "relative", height }}>
      <Bar data={chartData} options={options} />
    </div>
  );
}

function ToolErrorList({ traces }: { traces: HookTrace[] }) {
  const items = useMemo(() => {
    const map = new Map<string, number>();
    for (const t of traces) {
      if (t.hookStatus !== "failure") continue;
      const tool = t.toolName ?? "unknown";
      map.set(tool, (map.get(tool) ?? 0) + 1);
    }
    return Array.from(map.entries())
      .map(([key, value]) => ({ key, value }))
      .sort((a, b) => b.value - a.value);
  }, [traces]);

  return (
    <BarList
      items={items}
      barClassName="bg-red-500"
      renderLabel={(item) => <span className="truncate">{item.key}</span>}
    />
  );
}

function UniqueUsersTimeSeries({
  traces,
  from,
  to,
}: {
  traces: HookTrace[];
  from: Date;
  to: Date;
}) {
  const timeRangeMs = to.getTime() - from.getTime();

  const { labels, data } = useMemo(() => {
    if (traces.length === 0) return { labels: [], data: [] };

    const bucketMs =
      timeRangeMs <= 24 * 60 * 60 * 1000 ? 5 * 60 * 1000 : 60 * 60 * 1000;
    const buckets = new Map<number, Set<string>>();

    for (const t of traces) {
      const ms = Number(t.startTimeUnixNano) / 1_000_000;
      if (!ms || !t.userEmail) continue;
      const bucket = Math.floor(ms / bucketMs) * bucketMs;
      const users = buckets.get(bucket) ?? new Set<string>();
      users.add(t.userEmail);
      buckets.set(bucket, users);
    }

    const sorted = Array.from(buckets.entries()).sort((a, b) => a[0] - b[0]);
    return {
      labels: sorted.map(([ts]) => formatChartLabel(new Date(ts), timeRangeMs)),
      data: sorted.map(([, users]) => users.size),
    };
  }, [traces, timeRangeMs]);

  const chartData = {
    labels,
    datasets: [
      {
        label: " Active Users",
        data,
        borderColor: "#f59e0b",
        backgroundColor: "rgba(245, 158, 11, 0.1)",
        pointBackgroundColor: "#f59e0b",
        fill: true,
        tension: 0.45,
        borderWidth: 1.5,
        pointRadius: 0,
        pointHoverRadius: 4,
      },
    ],
  };

  const options = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: "index" as const, intersect: false },
    plugins: {
      legend: {
        position: "top" as const,
        align: "end" as const,
        labels: {
          boxWidth: 12,
          boxHeight: 12,
          useBorderRadius: true,
          borderRadius: 2,
          padding: 16,
          color: "#9ca3af",
          font: { size: 12 },
        },
      },
      tooltip: {
        backgroundColor: "rgba(0, 0, 0, 0.85)",
        titleColor: "#fff",
        bodyColor: "#e5e7eb",
        borderColor: "rgba(255, 255, 255, 0.1)",
        borderWidth: 1,
        padding: 12,
        boxPadding: 4,
        usePointStyle: true,
        callbacks: {
          label: (ctx: TooltipItem<"line">) =>
            ` Active Users: ${Math.round(ctx.parsed.y ?? 0)}`,
        },
      },
    },
    scales: {
      x: {
        grid: {
          display: true,
          color: "rgba(128, 128, 128, 0.1)",
          lineWidth: 1,
        },
        ticks: { maxTicksLimit: 8 },
      },
      y: {
        beginAtZero: true,
        grid: { color: "rgba(128, 128, 128, 0.2)" },
        ticks: { precision: 0 },
      },
    },
  };

  if (labels.length === 0) {
    return (
      <div className="text-muted-foreground flex h-24 items-center justify-center text-sm">
        No data
      </div>
    );
  }

  return (
    <div style={{ position: "relative", height: 200 }}>
      <Line data={chartData} options={options} />
    </div>
  );
}

function HooksAnalytics({
  groupedTraces,
  serverNameMappings,
  from,
  to,
}: {
  groupedTraces: HookTrace[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  from: Date;
  to: Date;
}) {
  const derivedServers = useMemo(() => {
    const map = new Map<
      string,
      { success: number; failure: number; tools: Set<string> }
    >();
    for (const t of groupedTraces) {
      const key = t.toolSource ?? "";
      const entry = map.get(key) ?? {
        success: 0,
        failure: 0,
        tools: new Set<string>(),
      };
      if (t.hookStatus === "success") entry.success += 1;
      else entry.failure += 1;
      if (t.toolName) entry.tools.add(t.toolName);
      map.set(key, entry);
    }
    return Array.from(map.entries()).map(
      ([serverName, { success, failure, tools }]) => ({
        serverName,
        eventCount: success + failure,
        successCount: success,
        failureCount: failure,
        failureRate: success + failure > 0 ? failure / (success + failure) : 0,
        uniqueTools: tools.size,
      }),
    );
  }, [groupedTraces]);

  const kpis = useMemo(() => {
    const avgSuccessRate =
      derivedServers.length > 0
        ? derivedServers.reduce(
            (sum, s) => sum + (1 - s.failureRate) * 100,
            0,
          ) / derivedServers.length
        : null;

    const totalEvents = derivedServers.reduce((s, r) => s + r.eventCount, 0);

    const activeUsers = new Set(
      groupedTraces.map((t) => t.userEmail).filter(Boolean),
    ).size;

    const activeSources = new Set(
      groupedTraces.map((t) => t.hookSource).filter(Boolean),
    ).size;

    const uniqueTools = derivedServers.reduce((s, r) => s + r.uniqueTools, 0);

    return {
      avgSuccessRate,
      totalEvents,
      activeUsers,
      activeSources,
      uniqueTools,
    };
  }, [derivedServers, groupedTraces]);

  const hasServers = derivedServers.length > 0;

  return (
    <div className="space-y-4">
      {/* KPI Cards */}
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-5">
        <MetricCard
          title="Avg Success Rate"
          value={kpis.avgSuccessRate ?? 0}
          format="percent"
          icon="circle-check"
          accentColor="green"
          subtext="across all servers"
        />
        <MetricCard
          title="Total Events"
          value={kpis.totalEvents}
          icon="activity"
          accentColor="purple"
          subtext="this period"
        />
        <MetricCard
          title="Active Users"
          value={kpis.activeUsers}
          icon="users"
          accentColor="yellow"
          subtext="from loaded traces"
        />
        <MetricCard
          title="Active Sources"
          value={kpis.activeSources}
          icon="monitor"
          accentColor="blue"
          subtext="distinct sources"
        />
        <MetricCard
          title="Unique Tools"
          value={kpis.uniqueTools}
          icon="wrench"
          accentColor="orange"
          subtext="across all servers"
        />
      </div>

      {/* Bar Charts */}
      {hasServers && (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
          <div className="border-border bg-card space-y-4 rounded-lg border p-4">
            <h3 className="text font-semibold">Activity by User</h3>
            <UserVolumeList traces={groupedTraces} />
          </div>
          <div className="border-border bg-card space-y-4 rounded-lg border p-4">
            <h3 className="text space-x-2 align-baseline font-semibold">
              Activity by Source
            </h3>
            <SourceVolumeChart traces={groupedTraces} />
          </div>
          <div className="border-border bg-card space-y-4 rounded-lg border p-4">
            <h3 className="text font-semibold">Activity by MCP Server</h3>
            <ServerActivityChart
              servers={derivedServers}
              serverNameMappings={serverNameMappings}
            />
          </div>
        </div>
      )}

      {/* Unique Users Over Time */}
      {groupedTraces.length > 0 && (
        <div className="border-border bg-card space-y-4 rounded-lg border p-4">
          <h3 className="text font-semibold">Active Users Over Time</h3>
          <UniqueUsersTimeSeries traces={groupedTraces} from={from} to={to} />
        </div>
      )}

      {/* Error Analysis */}
      {hasServers && (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <div className="border-border bg-card space-y-4 rounded-lg border p-4">
            <h3 className="text font-semibold">Servers by Error Rate</h3>
            <ServerErrorRateChart
              servers={derivedServers}
              serverNameMappings={serverNameMappings}
            />
          </div>
          <div className="border-border bg-card space-y-4 rounded-lg border p-4">
            <h3 className="text font-semibold">Tools by Error Count</h3>
            <ToolErrorList traces={groupedTraces} />
          </div>
        </div>
      )}
    </div>
  );
}
