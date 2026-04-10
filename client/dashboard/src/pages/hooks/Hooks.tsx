import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { EnterpriseGate } from "@/components/enterprise-gate";
import { InsightsSidebar } from "@/components/insights-sidebar";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { MultiSearch } from "@/components/ui/multi-search";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
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
import { telemetryListHooksTraces } from "@gram/client/funcs/telemetryListHooksTraces";
import type {
  HookTraceSummary as HookTrace,
  LogFilter,
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
import { List, Settings } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { LogDetailSheet } from "../logs/LogDetailSheet";
import { TraceLogsList } from "../logs/TraceLogsList";
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
  "#7dd3fc", // sky-300
  "#6ee7b7", // emerald-300
  "#c4b5fd", // violet-300
  "#fda4af", // rose-300
  "#fde047", // yellow-300
  "#5eead4", // teal-300
  "#a5b4fc", // indigo-300
  "#f0abfc", // fuchsia-300
  "#bef264", // lime-300
];

// ---------------------------------------------------------------------------
// Shared Chart.js config building blocks
// ---------------------------------------------------------------------------

// Derive deep-partial plugin types from ChartOptions (which uses _DeepPartialObject internally)
// so that partial objects are accepted by `satisfies` without needing all required properties.
type _BarLegend = Exclude<
  NonNullable<ChartOptions<"bar">["plugins"]>["legend"],
  false
>;
type _BarTooltip = NonNullable<ChartOptions<"bar">["plugins"]>["tooltip"];

const SHARED_LEGEND_LABELS = {
  boxWidth: 12,
  boxHeight: 12,
  useBorderRadius: true,
  borderRadius: 2,
  padding: 16,
  color: CHART_COLORS.label,
  font: { size: 12 },
} satisfies NonNullable<_BarLegend>["labels"];

const SHARED_LEGEND = {
  position: "top",
  align: "end",
  labels: SHARED_LEGEND_LABELS,
} satisfies NonNullable<_BarLegend>;

const SHARED_TOOLTIP = {
  backgroundColor: CHART_COLORS.tooltipBg,
  titleColor: CHART_COLORS.tooltipTitle,
  bodyColor: CHART_COLORS.tooltipBody,
  borderColor: CHART_COLORS.tooltipBorder,
  borderWidth: 1,
  padding: 12,
  boxPadding: 4,
} satisfies _BarTooltip;

// ---------------------------------------------------------------------------

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
    refetchLogs();
  }, [refetchLogs]);

  const isLogsDisabled = isLogsLogsDisabled;
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
  const [isLogsVisible, setIsLogsVisible] = useState(false);

  return (
    <>
      <div className="flex flex-col flex-1 min-h-0 w-full">
        <div className="px-8 pt-8 pb-4 flex flex-col gap-6 flex-1 min-h-0">
          {/* Header section */}
          <div className="flex items-start justify-between gap-4 shrink-0">
            <div className="flex flex-col gap-1 min-w-0">
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
          <div className="flex items-center gap-2 flex-wrap shrink-0">
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
            <Button
              variant={isLogsVisible ? "secondary" : "outline"}
              size="sm"
              className="h-[42px] shrink-0"
              onClick={() => setIsLogsVisible((v) => !v)}
            >
              <List className="h-4 w-4" />
              Logs
            </Button>
          </div>

          <div className="flex gap-4 flex-1 min-h-0 overflow-hidden">
            {/* Content Column */}

            <div className="flex-1 min-h-0 overflow-y-auto">
              <HooksAnalytics
                groupedTraces={groupedTraces}
                serverNameMappings={serverNameMappings}
                from={from}
                to={to}
                compact={isLogsVisible}
                addFilter={addFilter}
                onHookTypesChange={onHookTypesChange}
              />
            </div>

            {/* Logs Column */}
            {isLogsVisible && (
              <div className="flex-1 min-h-0 overflow-y-auto border">
                <div className="h-full flex flex-col bg-background">
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

type StackedBarDataset = {
  label: string;
  data: number[];
  backgroundColor: string;
  borderColor: string;
  borderWidth: number;
  barThickness: number;
  hoverBackgroundColor?: string;
  hoverBorderColor?: string;
};

function StackedBarChart({
  title,
  labels,
  datasets,
  handleFilter,
}: {
  title: string;
  labels: string[];
  datasets: StackedBarDataset[];
  handleFilter?: (datasetLabel: string, rowLabel: string) => void;
}) {
  if (labels.length === 0) return null;

  const barHeight = 24;
  const spacerHeight = 8;
  const containerHeight = Math.max(
    120,
    labels.length * (barHeight + spacerHeight) + 60,
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
    maintainAspectRatio: false,
    onClick(_, elements) {
      if (!elements.length || !handleFilter) return;
      const { datasetIndex, index } = elements[0];
      const datasetLabel = datasets[datasetIndex]?.label;
      const rowLabel = labels[index];
      if (datasetLabel && rowLabel) handleFilter(datasetLabel, rowLabel);
    },
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
        ticks: {
          color: CHART_COLORS.labelFaded,
          crossAlign: "far",
          padding: 2,
        },
        grid: { display: false },
      },
    },
    plugins: {
      legend: SHARED_LEGEND,
      tooltip: {
        ...SHARED_TOOLTIP,
        callbacks: {
          label: (item: TooltipItem<"bar">) =>
            ` ${item.dataset.label}: ${item.parsed.x}`,
        },
      },
    },
  };

  return (
    <div className="border-border bg-card space-y-4 rounded-lg border p-4">
      <h3 className="text font-semibold">{title}</h3>
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
    </div>
  );
}

function ServerActivityChart({
  title,
  traces,
  serverNameMappings,
  handleFilter,
}: {
  title: string;
  traces: HookTrace[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  handleFilter?: (userEmail: string, serverName: string) => void;
}) {
  const { labels, datasets } = useMemo(() => {
    // Build userEmail → toolSource → count
    const userMap = new Map<string, Map<string, number>>();
    const serverSet = new Set<string>();
    for (const t of traces) {
      const user = t.userEmail || "unknown";
      const server = t.toolSource ?? "";
      const displayName = !server
        ? (serverNameMappings.rawToDisplay.get("") ?? "Local Tools")
        : (serverNameMappings.rawToDisplay.get(server) ?? server);
      serverSet.add(displayName);
      const inner = userMap.get(user) ?? new Map<string, number>();
      inner.set(displayName, (inner.get(displayName) ?? 0) + 1);
      userMap.set(user, inner);
    }

    // Sort users by total count desc
    const sortedUsers = Array.from(userMap.entries())
      .map(([user, serverCounts]) => ({
        user,
        total: Array.from(serverCounts.values()).reduce((a, b) => a + b, 0),
        serverCounts,
      }))
      .sort((a, b) => b.total - a.total);

    // Sort servers by total usage desc for consistent legend ordering
    const sortedServers = Array.from(serverSet).sort((a, b) => {
      const aTotal = sortedUsers.reduce(
        (s, u) => s + (u.serverCounts.get(a) ?? 0),
        0,
      );
      const bTotal = sortedUsers.reduce(
        (s, u) => s + (u.serverCounts.get(b) ?? 0),
        0,
      );
      return bTotal - aTotal;
    });

    const chartLabels = sortedUsers.map((u) => u.user);
    const chartDatasets = sortedServers.map((server, i) => ({
      label: server,
      barThickness: 24,
      data: sortedUsers.map((u) => u.serverCounts.get(server) ?? 0),
      backgroundColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length] + "44",
      borderColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length],
      borderWidth: 1.5,
      hoverBackgroundColor:
        USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length] + "99",
      hoverBorderColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length],
    }));

    return { labels: chartLabels, datasets: chartDatasets };
  }, [traces, serverNameMappings.rawToDisplay]);

  return (
    <StackedBarChart
      title={title}
      labels={labels}
      datasets={datasets}
      handleFilter={handleFilter}
    />
  );
}

function SourceVolumeChart({
  title,
  traces,
  serverNameMappings,
  handleFilter,
}: {
  title: string;
  traces: HookTrace[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  handleFilter?: (serverName: string, source: string) => void;
}) {
  const { labels, datasets } = useMemo(() => {
    // Build toolSource (display name) → hookSource → count
    const serverMap = new Map<string, Map<string, number>>();
    const sourceSet = new Set<string>();
    for (const t of traces) {
      const raw = t.toolSource ?? "";
      const displayName = !raw
        ? (serverNameMappings.rawToDisplay.get("") ?? "Local Tools")
        : (serverNameMappings.rawToDisplay.get(raw) ?? raw);
      const source = t.hookSource || "unknown";
      sourceSet.add(source);
      const inner = serverMap.get(displayName) ?? new Map<string, number>();
      inner.set(source, (inner.get(source) ?? 0) + 1);
      serverMap.set(displayName, inner);
    }

    // Sort servers by total count desc
    const sortedServers = Array.from(serverMap.entries())
      .map(([displayName, sourceCounts]) => ({
        displayName,
        total: Array.from(sourceCounts.values()).reduce((a, b) => a + b, 0),
        sourceCounts,
      }))
      .sort((a, b) => b.total - a.total);

    // Sort sources by total usage desc, unknown last, for consistent legend ordering
    const sortedSources = Array.from(sourceSet).sort((a, b) => {
      if (a === "unknown") return 1;
      if (b === "unknown") return -1;
      const aTotal = sortedServers.reduce(
        (s, srv) => s + (srv.sourceCounts.get(a) ?? 0),
        0,
      );
      const bTotal = sortedServers.reduce(
        (s, srv) => s + (srv.sourceCounts.get(b) ?? 0),
        0,
      );
      return bTotal - aTotal;
    });

    const chartLabels = sortedServers.map((s) => s.displayName);
    const chartDatasets = sortedSources.map((source, i) => ({
      label: source,
      barThickness: 24,
      data: sortedServers.map((s) => s.sourceCounts.get(source) ?? 0),
      backgroundColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length] + "44",
      borderColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length],
      borderWidth: 1.5,
      hoverBackgroundColor:
        USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length] + "99",
      hoverBorderColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length],
    }));

    return { labels: chartLabels, datasets: chartDatasets };
  }, [traces, serverNameMappings.rawToDisplay]);

  return (
    <StackedBarChart
      title={title}
      labels={labels}
      datasets={datasets}
      handleFilter={handleFilter}
    />
  );
}

// Shared ranked bar list used by volume/error breakdown charts

function UserVolumeList({
  title,
  traces,
  handleFilter,
}: {
  title: string;
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
      backgroundColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length] + "44",
      borderColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length],
      borderWidth: 1.5,
      hoverBackgroundColor:
        USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length] + "99",
      hoverBorderColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length],
    }));

    return { labels: chartLabels, datasets: chartDatasets };
  }, [traces]);

  return (
    <StackedBarChart
      title={title}
      labels={labels}
      datasets={datasets}
      handleFilter={handleFilter}
    />
  );
}

function ServerErrorRateChart({
  title,
  traces,
  serverNameMappings,
}: {
  title: string;
  traces: HookTrace[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
}) {
  const { labels, datasets } = useMemo(() => {
    // Build: serverDisplay → toolName → errorCount (failures only)
    const serverMap = new Map<string, Map<string, number>>();
    const toolSet = new Set<string>();
    for (const t of traces) {
      if (t.hookStatus !== "failure") continue;
      const raw = t.toolSource ?? "";
      const displayName = !raw
        ? (serverNameMappings.rawToDisplay.get("") ?? "Local Tools")
        : (serverNameMappings.rawToDisplay.get(raw) ?? raw);
      const tool = t.toolName ?? "unknown";
      toolSet.add(tool);
      const inner = serverMap.get(displayName) ?? new Map<string, number>();
      inner.set(tool, (inner.get(tool) ?? 0) + 1);
      serverMap.set(displayName, inner);
    }

    // Sort servers by total error count desc
    const sortedServers = Array.from(serverMap.entries())
      .map(([displayName, toolCounts]) => ({
        displayName,
        total: Array.from(toolCounts.values()).reduce((a, b) => a + b, 0),
        toolCounts,
      }))
      .sort((a, b) => b.total - a.total);

    // Sort tools by total errors desc
    const sortedTools = Array.from(toolSet).sort((a, b) => {
      const aTotal = sortedServers.reduce(
        (s, srv) => s + (srv.toolCounts.get(a) ?? 0),
        0,
      );
      const bTotal = sortedServers.reduce(
        (s, srv) => s + (srv.toolCounts.get(b) ?? 0),
        0,
      );
      return bTotal - aTotal;
    });

    const chartLabels = sortedServers.map((s) => s.displayName);
    const errorColor = "#ef4444";
    const chartDatasets = sortedTools.map((tool) => ({
      label: tool,
      barThickness: 16,
      data: sortedServers.map((s) => s.toolCounts.get(tool) ?? 0),
      backgroundColor: errorColor + "1a",
      borderColor: errorColor,
      borderWidth: 1.5,
      hoverBackgroundColor: errorColor + "33",
      hoverBorderColor: errorColor,
    }));

    return { labels: chartLabels, datasets: chartDatasets };
  }, [traces, serverNameMappings.rawToDisplay]);

  const height = Math.max(120, labels.length * (24 + 8) + 60);

  const options: ChartOptions<"bar"> = {
    indexAxis: "y",
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
      legend: { display: false },
      tooltip: {
        ...SHARED_TOOLTIP,
        callbacks: {
          title: (items) => items[0]?.label ?? "",
          label: (ctx: TooltipItem<"bar">) =>
            ` ${ctx.dataset.label}: ${(ctx.parsed.x ?? 0).toLocaleString()} errors`,
        },
      },
    },
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
        grid: { display: false },
        ticks: { color: CHART_COLORS.labelFaded, font: { size: 12 } },
      },
    },
  };

  return (
    <div className="border-border bg-card space-y-4 rounded-lg border p-4">
      <h3 className="text font-semibold">{title}</h3>
      {labels.length === 0 ? (
        <div className="text-muted-foreground flex h-16 items-center justify-center text-sm">
          No errors in this period
        </div>
      ) : (
        <div style={{ position: "relative", height }}>
          <Bar data={{ labels, datasets }} options={options} />
        </div>
      )}
    </div>
  );
}

function UserErrorChart({
  title,
  traces,
  serverNameMappings,
}: {
  title: string;
  traces: HookTrace[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
}) {
  const { labels, datasets } = useMemo(() => {
    const userMap = new Map<string, Map<string, number>>();
    const serverSet = new Set<string>();
    for (const t of traces) {
      if (t.hookStatus !== "failure") continue;
      const user = t.userEmail || "unknown";
      const raw = t.toolSource ?? "";
      const displayName = !raw
        ? (serverNameMappings.rawToDisplay.get("") ?? "Local Tools")
        : (serverNameMappings.rawToDisplay.get(raw) ?? raw);
      serverSet.add(displayName);
      const inner = userMap.get(user) ?? new Map<string, number>();
      inner.set(displayName, (inner.get(displayName) ?? 0) + 1);
      userMap.set(user, inner);
    }

    const sortedUsers = Array.from(userMap.entries())
      .map(([user, serverCounts]) => ({
        user,
        total: Array.from(serverCounts.values()).reduce((a, b) => a + b, 0),
        serverCounts,
      }))
      .sort((a, b) => b.total - a.total);

    const sortedServers = Array.from(serverSet).sort((a, b) => {
      const aTotal = sortedUsers.reduce(
        (s, u) => s + (u.serverCounts.get(a) ?? 0),
        0,
      );
      const bTotal = sortedUsers.reduce(
        (s, u) => s + (u.serverCounts.get(b) ?? 0),
        0,
      );
      return bTotal - aTotal;
    });

    const chartLabels = sortedUsers.map((u) => u.user);
    const chartDatasets = sortedServers.map((server, i) => ({
      label: server,
      barThickness: 24,
      data: sortedUsers.map((u) => u.serverCounts.get(server) ?? 0),
      backgroundColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length] + "1a",
      borderColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length],
      borderWidth: 1.5,
      hoverBackgroundColor:
        USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length] + "33",
      hoverBorderColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length],
    }));

    return { labels: chartLabels, datasets: chartDatasets };
  }, [traces, serverNameMappings.rawToDisplay]);

  if (labels.length === 0) {
    return (
      <div className="border-border bg-card space-y-4 rounded-lg border p-4">
        <h3 className="text font-semibold">{title}</h3>
        <div className="text-muted-foreground flex h-16 items-center justify-center text-sm">
          No errors in this period
        </div>
      </div>
    );
  }

  return <StackedBarChart title={title} labels={labels} datasets={datasets} />;
}

function buildMultiLineData(
  traces: HookTrace[],
  keyFn: (t: HookTrace) => string | null | undefined,
  timeRangeMs: number,
) {
  if (traces.length === 0)
    return { labels: [], tooltipLabels: [], datasets: [] };

  const bucketMs =
    timeRangeMs <= 24 * 60 * 60 * 1000 ? 5 * 60 * 1000 : 60 * 60 * 1000;
  const seriesMap = new Map<string, Map<number, number>>();

  for (const t of traces) {
    const ms = Number(t.startTimeUnixNano) / 1_000_000;
    if (!ms) continue;
    const key = keyFn(t);
    if (!key) continue;
    const bucket = Math.floor(ms / bucketMs) * bucketMs;
    const series = seriesMap.get(key) ?? new Map<number, number>();
    series.set(bucket, (series.get(bucket) ?? 0) + 1);
    seriesMap.set(key, series);
  }

  if (seriesMap.size === 0)
    return { labels: [], tooltipLabels: [], datasets: [] };

  const allTimestamps = new Set<number>();
  for (const series of seriesMap.values()) {
    for (const ts of series.keys()) allTimestamps.add(ts);
  }
  const sortedTs = Array.from(allTimestamps).sort((a, b) => a - b);
  const labels = sortedTs.map((ts) =>
    formatChartLabel(new Date(ts), timeRangeMs),
  );
  const tooltipLabels = sortedTs.map((ts) =>
    new Date(ts).toLocaleString([], {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    }),
  );

  const datasets = Array.from(seriesMap.entries()).map(([key, series], i) => {
    const color = USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length];
    return {
      label: key,
      data: sortedTs.map((ts) => series.get(ts) ?? 0),
      borderColor: color,
      backgroundColor: color + "1a",
      pointBackgroundColor: color,
      fill: false,
      tension: 0.45,
      borderWidth: 1.5,
      pointRadius: 0,
      pointHoverRadius: 4,
    };
  });

  return { labels, tooltipLabels, datasets };
}

function MultiLineChart({
  labels,
  tooltipLabels,
  datasets,
}: {
  labels: string[];
  tooltipLabels: string[];
  datasets: ReturnType<typeof buildMultiLineData>["datasets"];
}) {
  if (labels.length === 0) {
    return (
      <div className="text-muted-foreground flex h-24 items-center justify-center text-sm">
        No data
      </div>
    );
  }

  const options: ChartOptions<"line"> = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: "index", intersect: false },
    plugins: {
      legend: SHARED_LEGEND,
      tooltip: {
        ...SHARED_TOOLTIP,
        callbacks: {
          title: (items) => tooltipLabels[items[0]?.dataIndex ?? 0] ?? "",
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

  return (
    <div style={{ position: "relative", height: 200 }}>
      <Line data={{ labels, datasets }} options={options} />
    </div>
  );
}

function ServerUsageTimeSeries({
  traces,
  from,
  to,
  serverNameMappings,
}: {
  traces: HookTrace[];
  from: Date;
  to: Date;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
}) {
  const timeRangeMs = to.getTime() - from.getTime();
  const { labels, tooltipLabels, datasets } = useMemo(
    () =>
      buildMultiLineData(
        traces,
        (t) => {
          const raw = t.toolSource ?? "";
          return !raw
            ? (serverNameMappings.rawToDisplay.get("") ?? "Local Tools")
            : (serverNameMappings.rawToDisplay.get(raw) ?? raw);
        },
        timeRangeMs,
      ),
    [traces, timeRangeMs, serverNameMappings.rawToDisplay],
  );
  return (
    <MultiLineChart
      labels={labels}
      tooltipLabels={tooltipLabels}
      datasets={datasets}
    />
  );
}

function UserUsageTimeSeries({
  traces,
  from,
  to,
}: {
  traces: HookTrace[];
  from: Date;
  to: Date;
}) {
  const timeRangeMs = to.getTime() - from.getTime();
  const { labels, tooltipLabels, datasets } = useMemo(
    () => buildMultiLineData(traces, (t) => t.userEmail, timeRangeMs),
    [traces, timeRangeMs],
  );
  return (
    <MultiLineChart
      labels={labels}
      tooltipLabels={tooltipLabels}
      datasets={datasets}
    />
  );
}

function HooksAnalytics({
  groupedTraces,
  serverNameMappings,
  from,
  to,
  compact = false,
  addFilter,
  onHookTypesChange,
}: {
  groupedTraces: HookTrace[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  from: Date;
  to: Date;
  compact?: boolean;
  addFilter: (chip: FilterChip) => void;
  onHookTypesChange: (types: TypesToInclude[]) => void;
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
      else if (t.hookStatus === "failure") entry.failure += 1;
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
    const totalEvents = derivedServers.reduce((s, r) => s + r.eventCount, 0);

    const totalSuccesses = derivedServers.reduce(
      (s, r) => s + r.successCount,
      0,
    );
    const avgSuccessRate =
      totalEvents > 0 ? (totalSuccesses / totalEvents) * 100 : null;

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

  type FilterAxisConfig = Partial<Record<"user" | "server", "dataset" | "row">>;

  const makeFilterHandler = useCallback(
    (config: FilterAxisConfig) => (datasetLabel: string, rowLabel: string) => {
      const localToolsDisplayName =
        serverNameMappings.rawToDisplay.get("") ?? "Local Tools";
      const apply = (value: string, filterType: "server" | "user") => {
        if (!value || value === "unknown") return;
        if (filterType === "server") {
          if (value === localToolsDisplayName) {
            onHookTypesChange(["local"]);
            return;
          }
          const rawFilters = serverNameMappings.displayToRaws.get(value) ?? [
            value,
          ];
          addFilter({
            display: value,
            filters: rawFilters,
            path: "gram.tool_call.source",
          });
        } else {
          addFilter({ display: value, filters: [value], path: "user.email" });
        }
      };
      for (const [filterType, axis] of Object.entries(config) as [
        "server" | "user",
        "dataset" | "row",
      ][]) {
        apply(axis === "dataset" ? datasetLabel : rowLabel, filterType);
      }
    },
    [
      addFilter,
      onHookTypesChange,
      serverNameMappings.rawToDisplay,
      serverNameMappings.displayToRaws,
    ],
  );

  return (
    <div className="space-y-4">
      {/* KPI Cards */}
      <div
        className={cn(
          "grid gap-3",
          compact
            ? "grid-cols-2 md:grid-cols-3"
            : "grid-cols-2 md:grid-cols-3 lg:grid-cols-5",
        )}
      >
        <MetricCard
          title="Avg Success Rate"
          value={kpis.avgSuccessRate ?? 0}
          format="percent"
          icon="circle-check"
          accentColor="green"
          subtext="from loaded traces"
        />
        <MetricCard
          title="Total Events"
          value={kpis.totalEvents}
          icon="activity"
          accentColor="purple"
          subtext="from loaded traces"
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
          subtext="from loaded traces"
        />
        <MetricCard
          title="Unique Tools"
          value={kpis.uniqueTools}
          icon="wrench"
          accentColor="orange"
          subtext="from loaded traces"
        />
      </div>

      {/* Bar Charts */}
      {hasServers && (
        <div
          className={cn(
            "grid gap-4",
            compact ? "grid-cols-1" : "grid-cols-1 lg:grid-cols-3",
          )}
        >
          <UserVolumeList
            title="Source Usage per User"
            traces={groupedTraces}
            handleFilter={makeFilterHandler({ user: "row" })}
          />
          <ServerActivityChart
            title="Server Usage per User"
            traces={groupedTraces}
            serverNameMappings={serverNameMappings}
            handleFilter={makeFilterHandler({ server: "dataset", user: "row" })}
          />
          <SourceVolumeChart
            title="Source Usage per MCP Server"
            traces={groupedTraces}
            serverNameMappings={serverNameMappings}
            handleFilter={makeFilterHandler({ server: "row" })}
          />
        </div>
      )}

      {/* Usage Over Time */}
      {groupedTraces.length > 0 && (
        <div
          className={cn(
            "grid gap-4",
            compact ? "grid-cols-1" : "grid-cols-1 lg:grid-cols-2",
          )}
        >
          <div className="rounded-lg border border-border bg-card p-4 space-y-4">
            <h3 className="text font-semibold">Server Usage</h3>
            <ServerUsageTimeSeries
              traces={groupedTraces}
              from={from}
              to={to}
              serverNameMappings={serverNameMappings}
            />
          </div>
          <div className="border-border bg-card space-y-4 rounded-lg border p-4">
            <h3 className="text font-semibold">User Usage</h3>
            <UserUsageTimeSeries traces={groupedTraces} from={from} to={to} />
          </div>
        </div>
      )}

      {/* Error Analysis */}
      {hasServers && (
        <div
          className={cn(
            "grid gap-4",
            compact ? "grid-cols-1" : "grid-cols-1 lg:grid-cols-2",
          )}
        >
          <ServerErrorRateChart
            title="Errors per Server and Tool"
            traces={groupedTraces}
            serverNameMappings={serverNameMappings}
          />
          <UserErrorChart
            title="Errors per User"
            traces={groupedTraces}
            serverNameMappings={serverNameMappings}
          />
        </div>
      )}
    </div>
  );
}
