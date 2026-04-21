import { EditServerNameDialog } from "./EditServerNameDialog";
import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { EnterpriseGate } from "@/components/enterprise-gate";
import { InsightsConfig } from "@/components/insights-sidebar";
import { RequireScope } from "@/components/require-scope";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { Page } from "@/components/page-layout";
import { ErrorAlert } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
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
import { telemetryGetHooksSummary } from "@gram/client/funcs/telemetryGetHooksSummary";
import { telemetryListHooksTraces } from "@gram/client/funcs/telemetryListHooksTraces";
import type {
  GetHooksSummaryResult,
  HooksBreakdownRow,
  HooksTimeSeriesPoint,
  HookTraceSummary as HookTrace,
  LogFilter,
  TelemetryLogRecord,
  TypesToInclude,
} from "@gram/client/models/components";
import { useGramContext } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Icon } from "@speakeasy-api/moonshine";
import { ChartCard } from "@/components/chart/ChartCard";
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
  type Scale,
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
  "#60a5fa", // blue-400
  "#fb923c", // orange-400
  "#34d399", // emerald-400
  "#f87171", // red-400
  "#a78bfa", // violet-400
  "#facc15", // yellow-400
  "#22d3ee", // cyan-400
  "#f472b6", // pink-400
  "#a3e635", // lime-400
];

const BRAND_RED_COLORS = [
  "#fb923c", // orange-400
  "#ea580c", // orange-600
  "#dc2626", // red-600
  "#b91c1c", // red-700
  "#991b1b", // red-800
  "#7f1d1d", // red-900
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
type _BarScales = NonNullable<ChartOptions<"bar">["scales"]>;

const SHARED_LEGEND = {
  display: false,
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

const SHARED_BAR_SCALES = {
  x: {
    stacked: true,
    grid: { color: CHART_COLORS.gridLine },
    ticks: { color: CHART_COLORS.labelFaded, precision: 0 },
    afterFit(scale: Scale) {
      scale.paddingRight = 30;
    },
  },
  y: {
    stacked: true,
    grid: { display: false },
    ticks: {
      color: CHART_COLORS.labelFaded,
      crossAlign: "far" as const,
      padding: 2,
      font: { size: 12 },
      callback(value) {
        const label = this.getLabelForValue(value as number);
        const display = label.includes("@")
          ? label.split("@")[0]!.slice(0, 14) + "@…"
          : label.slice(0, 14) + (label.length > 14 ? "…" : "");
        return display;
      },
    },
  },
} satisfies _BarScales;

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
  return (
    <RequireScope scope={["build:read", "build:write"]} level="page">
      <HooksContent />
    </RequireScope>
  );
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
  const defaultHookTypes: TypesToInclude[] = ["mcp", "skill"];
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

  // Fetch complete aggregated summary for accurate analytics (not limited by pagination)
  const {
    data: summaryData,
    isPending: summaryPending,
    isError: summaryIsError,
  } = useQuery({
    queryKey: [
      "hooks-summary",
      from.toISOString(),
      to.toISOString(),
      logFilters,
      selectedHookTypes,
    ],
    queryFn: () =>
      unwrapAsync(
        telemetryGetHooksSummary(client, {
          getHooksSummaryPayload: {
            from,
            to,
            filters: logFilters,
            typesToInclude:
              selectedHookTypes.length > 0 ? selectedHookTypes : undefined,
          },
        }),
      ),
    throwOnError: false,
  });

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
          const isDefault =
            types.length === 2 &&
            types.includes("mcp") &&
            types.includes("skill") &&
            !types.includes("local");
          if (isDefault) {
            // Default selection (mcp + skill) - remove param
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
    <>
      <InsightsConfig
        mcpConfig={mcpConfig}
        title="Explore Hooks"
        subtitle="Ask me about your hooks! Powered by Elements + Gram MCP"
        hideTrigger={isLogsDisabled}
      />
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
                  summaryData={summaryData}
                  summaryPending={summaryPending}
                  summaryIsError={summaryIsError}
                />
              </EnterpriseGate>
            </Page.Body>
          )}
        </Page>
      </div>
    </>
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
  summaryData,
  summaryPending,
  summaryIsError,
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
  summaryData: GetHooksSummaryResult | undefined;
  summaryPending: boolean;
  summaryIsError: boolean;
}) {
  const orgRoutes = useOrgRoutes();
  const { from, to } = useMemo(
    () => customRange ?? getPresetRange(dateRange),
    [customRange, dateRange],
  );
  const [isLogsVisible, setIsLogsVisible] = useState(false);

  return (
    <>
      <div className="flex min-h-0 w-full flex-1 flex-col">
        <div className="flex min-h-0 flex-1 flex-col gap-6 px-8 pt-8 pb-4">
          {/* Header section */}
          <div className="flex shrink-0 items-start justify-between gap-4">
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
          <div className="flex shrink-0 flex-wrap items-center gap-2">
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

          <div className="flex min-h-0 flex-1 gap-4 overflow-hidden">
            {/* Content Column */}

            <div className="min-h-0 flex-1 overflow-y-auto">
              {error ? (
                <ErrorAlert
                  error={error}
                  title="Error loading hook events"
                  className="mx-auto w-full"
                />
              ) : isLoading ? (
                <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
                  <Spinner className="mr-0 size-5" />
                  <span>Loading hook events...</span>
                </div>
              ) : groupedTraces.length === 0 && activeFilters.length === 0 ? (
                <HooksEmptyState />
              ) : groupedTraces.length === 0 ? (
                <div className="py-12 text-center">
                  <div className="flex flex-col items-center gap-3">
                    <div className="bg-muted flex size-12 items-center justify-center rounded-full">
                      <Icon
                        name="inbox"
                        className="text-muted-foreground size-6"
                      />
                    </div>
                    <span className="text-foreground font-medium">
                      No matching hook events
                    </span>
                    <span className="text-muted-foreground max-w-sm text-sm">
                      Try adjusting your search query or time range
                    </span>
                  </div>
                </div>
              ) : (
                <HooksAnalytics
                  serverNameMappings={serverNameMappings}
                  from={from}
                  to={to}
                  compact={isLogsVisible}
                  addFilter={addFilter}
                  onHookTypesChange={onHookTypesChange}
                  summaryData={summaryData}
                  summaryPending={summaryPending}
                  summaryIsError={summaryIsError}
                />
              )}
            </div>

            {/* Logs Column */}
            {isLogsVisible && (
              <div className="min-h-0 flex-1 overflow-y-auto border">
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
  {
    label: "MCP Servers",
    labelShort: "Servers",
    value: "mcp" as TypesToInclude,
  },
  {
    label: "Local Tools",
    labelShort: "Local",
    value: "local" as TypesToInclude,
  },
  { label: "Skills", labelShort: "Skills", value: "skill" as TypesToInclude },
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
      return `Showing ${selected?.labelShort || selectedHookTypes[0]}`;
    }

    const labels = HOOK_TYPE_OPTIONS.filter((opt) =>
      selectedHookTypes.includes(opt.value),
    ).map((opt) => opt.labelShort);
    return `Showing ${labels.join(" & ")}`;
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
      <ErrorAlert
        error={error}
        title="Error loading hook events"
        className="m-4"
      />
    );
  }

  if (isLoading) {
    return (
      <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
        <Spinner className="mr-0 size-5" />
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
  const [editDialogOpen, setEditDialogOpen] = useState(false);
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

  const editDialogProps = useMemo(() => {
    if (!serverName) return null;
    const overrides =
      serverNameMappings.displayToOverrides.get(displayServerName ?? "") ?? [];
    const hasOverride = overrides.some((o) => o.rawServerName === serverName);
    return {
      serverName: displayServerName ?? serverName,
      groupedOverrides: overrides,
      unmappedRawName: hasOverride ? null : serverName,
    };
  }, [serverName, displayServerName, serverNameMappings.displayToOverrides]);

  const serverNameBadge = useMemo(() => {
    // For skills, show [Skill] badge
    if (toolName === "Skill" && skillName) {
      return (
        <span className="shrink-0 truncate rounded-xs bg-purple-500/10 px-2 py-1 font-mono text-xs font-medium text-purple-600 dark:text-purple-400">
          Skill
        </span>
      );
    }

    const isLocal = !serverName;
    return (
      <span
        className={cn(
          "shrink-0 truncate rounded-xs px-2 py-1 font-mono text-xs",
          isLocal
            ? "bg-muted/50 text-muted-foreground"
            : "bg-primary/10 text-primary font-medium",
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
      <div
        role="button"
        tabIndex={0}
        onClick={onToggle}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") onToggle();
        }}
        className="hover:bg-muted/50 flex w-full cursor-pointer items-center gap-3 px-5 py-2.5 text-left transition-colors"
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
          <div className="group/server relative flex shrink-0 items-center">
            {serverNameBadge}
            {serverName && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  setEditDialogOpen(true);
                }}
                className="text-muted-foreground hover:text-foreground bg-card hover:bg-muted border-border invisible absolute -right-6 size-6 rounded border p-1 shadow-sm transition-colors group-hover/server:visible"
                aria-label="Edit display name"
              >
                <Icon name="pencil" className="size-3" />
              </button>
            )}
          </div>
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
      </div>

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

      {editDialogProps && (
        <EditServerNameDialog
          open={editDialogOpen}
          onOpenChange={setEditDialogOpen}
          serverName={editDialogProps.serverName}
          groupedOverrides={editDialogProps.groupedOverrides}
          unmappedRawName={editDialogProps.unmappedRawName}
          upsert={serverNameMappings.upsert}
          remove={serverNameMappings.remove}
          isUpserting={serverNameMappings.isUpserting}
          isDeleting={serverNameMappings.isDeleting}
        />
      )}
    </div>
  );
}

type StackedBarDataset = {
  label: string;
  data: number[];
  backgroundColor: string;
  borderColor?: string;
  borderWidth?: number;
  barThickness: number;
  hoverBackgroundColor?: string;
  hoverBorderColor?: string;
};

const stackTotalPlugin = {
  id: "stackTotal",
  afterDatasetsDraw(chart: ChartJS) {
    const { ctx, data } = chart;
    const lastMeta = chart.getDatasetMeta(data.datasets.length - 1);
    ctx.save();
    ctx.font = "12px sans-serif";
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

const STACKED_BAR_PLUGINS = [stackTotalPlugin];

function StackedBarChart({
  labels,
  datasets,
  handleFilter,
  expanded = false,
}: {
  labels: string[];
  datasets: StackedBarDataset[];
  handleFilter?: (datasetLabel: string, rowLabel: string) => void;
  expanded?: boolean;
}) {
  const barHeight = expanded ? 36 : 24;
  const spacerHeight = expanded ? 12 : 8;
  const containerHeight = Math.max(
    120,
    labels.length * (barHeight + spacerHeight) + 60,
  );

  const options = useMemo<ChartOptions<"bar">>(
    () => ({
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
      onHover(event, elements) {
        const el = event.native?.target as HTMLElement | null;
        if (el) el.style.cursor = elements.length ? "pointer" : "default";
      },
      scales: SHARED_BAR_SCALES,
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
    }),
    [datasets, labels, handleFilter],
  );

  if (labels.length === 0) return null;

  return (
    <div style={{ height: containerHeight }}>
      <Bar
        plugins={STACKED_BAR_PLUGINS}
        data={{ labels, datasets }}
        options={options}
      />
    </div>
  );
}

function UsersPerServerChart({
  title,
  breakdown,
  serverNameMappings,
  handleFilter,
  expandedChart,
  onExpand,
}: {
  title: string;
  breakdown: HooksBreakdownRow[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  handleFilter?: (userEmail: string, serverName: string) => void;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const chartId = "users-per-server";
  const expanded = expandedChart === chartId;
  const { labels, datasets } = useMemo(() => {
    const serverMap = new Map<string, Map<string, number>>();
    const userSet = new Set<string>();
    for (const row of breakdown) {
      const user = row.userEmail || "unknown";
      const displayName =
        row.serverName === "local"
          ? (serverNameMappings.rawToDisplay.get("") ?? "Local Tools")
          : (serverNameMappings.rawToDisplay.get(row.serverName) ??
            row.serverName);
      userSet.add(user);
      const inner = serverMap.get(displayName) ?? new Map<string, number>();
      inner.set(user, (inner.get(user) ?? 0) + row.eventCount);
      serverMap.set(displayName, inner);
    }

    const sortedServers = Array.from(serverMap.entries())
      .map(([server, userCounts]) => ({
        server,
        total: Array.from(userCounts.values()).reduce((a, b) => a + b, 0),
        userCounts,
      }))
      .sort((a, b) => b.total - a.total);

    const sortedUsers = Array.from(userSet).sort((a, b) => {
      const aTotal = sortedServers.reduce(
        (s, srv) => s + (srv.userCounts.get(a) ?? 0),
        0,
      );
      const bTotal = sortedServers.reduce(
        (s, srv) => s + (srv.userCounts.get(b) ?? 0),
        0,
      );
      return bTotal - aTotal;
    });

    const chartLabels = sortedServers.map((s) => s.server);
    const chartDatasets = sortedUsers.map((user, i) => ({
      label: user,
      barThickness: 24,
      data: sortedServers.map((s) => s.userCounts.get(user) ?? 0),
      backgroundColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length],
      hoverBackgroundColor:
        USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length] + "cc",
    }));

    return { labels: chartLabels, datasets: chartDatasets };
  }, [breakdown, serverNameMappings.rawToDisplay]);

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
    >
      <StackedBarChart
        labels={labels}
        datasets={datasets}
        handleFilter={handleFilter}
        expanded={expanded}
      />
    </ChartCard>
  );
}

function UserEventCountsChart({
  title,
  breakdown,
  handleFilter,
  expandedChart,
  onExpand,
}: {
  title: string;
  breakdown: HooksBreakdownRow[];
  handleFilter?: (datasetLabel: string, userEmail: string) => void;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const chartId = "user-event-counts";
  const expanded = expandedChart === chartId;
  const { labels, datasets } = useMemo(() => {
    const userMap = new Map<string, number>();
    for (const row of breakdown) {
      const user = row.userEmail || "unknown";
      userMap.set(user, (userMap.get(user) ?? 0) + row.eventCount);
    }

    const sortedUsers = Array.from(userMap.entries()).sort(
      (a, b) => b[1] - a[1],
    );

    const chartLabels = sortedUsers.map(([user]) => user);
    const color = USER_SOURCE_COLORS[0]!;
    const chartDatasets = [
      {
        label: "Events",
        barThickness: 24,
        data: sortedUsers.map(([, count]) => count),
        backgroundColor: color,
        hoverBackgroundColor: color + "cc",
      },
    ];

    return { labels: chartLabels, datasets: chartDatasets };
  }, [breakdown]);

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
    >
      <StackedBarChart
        labels={labels}
        datasets={datasets}
        handleFilter={handleFilter}
        expanded={expanded}
      />
    </ChartCard>
  );
}

function ServerErrorRateChart({
  title,
  breakdown,
  serverNameMappings,
  expandedChart,
  onExpand,
}: {
  title: string;
  breakdown: HooksBreakdownRow[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const chartId = "errors-per-server";
  const expanded = expandedChart === chartId;
  const { labels, datasets } = useMemo(() => {
    // Build: serverDisplay → toolName → failureCount (failures only)
    const serverMap = new Map<string, Map<string, number>>();
    const toolSet = new Set<string>();
    for (const row of breakdown) {
      if (row.failureCount === 0) continue;
      const displayName =
        row.serverName === "local"
          ? (serverNameMappings.rawToDisplay.get("") ?? "Local Tools")
          : (serverNameMappings.rawToDisplay.get(row.serverName) ??
            row.serverName);
      const tool = row.toolName || "unknown";
      toolSet.add(tool);
      const inner = serverMap.get(displayName) ?? new Map<string, number>();
      inner.set(tool, (inner.get(tool) ?? 0) + row.failureCount);
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
    const chartDatasets = sortedTools.map((tool, i) => ({
      label: tool,
      barThickness: 16,
      data: sortedServers.map((s) => s.toolCounts.get(tool) ?? 0),
      backgroundColor: BRAND_RED_COLORS[i % BRAND_RED_COLORS.length],
      hoverBackgroundColor:
        BRAND_RED_COLORS[i % BRAND_RED_COLORS.length] + "cc",
    }));

    return { labels: chartLabels, datasets: chartDatasets };
  }, [breakdown, serverNameMappings.rawToDisplay]);

  const barHeight = expanded ? 36 : 24;
  const spacerHeight = expanded ? 12 : 8;
  const height = Math.max(120, labels.length * (barHeight + spacerHeight) + 60);

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
            `${ctx.dataset.label}: ${(ctx.parsed.x ?? 0).toLocaleString()}`,
        },
      },
    },
    scales: SHARED_BAR_SCALES,
  };

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
    >
      {labels.length === 0 ? (
        <div className="text-muted-foreground flex h-16 items-center justify-center text-sm">
          No errors in this period
        </div>
      ) : (
        <div style={{ position: "relative", height }}>
          <Bar data={{ labels, datasets }} options={options} />
        </div>
      )}
    </ChartCard>
  );
}

function buildTimeSeriesFromSummary(
  timeSeries: HooksTimeSeriesPoint[],
  keyFn: (p: HooksTimeSeriesPoint) => string,
  timeRangeMs: number,
  valueFn: (p: HooksTimeSeriesPoint) => number = (p) => p.eventCount,
) {
  if (timeSeries.length === 0)
    return { labels: [], tooltipLabels: [], datasets: [] };

  const seriesMap = new Map<string, Map<number, number>>();

  for (const pt of timeSeries) {
    const key = keyFn(pt);
    if (!key) continue;
    // Use BigInt conversion to avoid precision loss for ns timestamps
    const ms = Number(BigInt(pt.bucketStartNs) / BigInt(1_000_000));
    const series = seriesMap.get(key) ?? new Map<number, number>();
    series.set(ms, (series.get(ms) ?? 0) + valueFn(pt));
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
  tooltipAfterBody,
  height = 200,
}: {
  labels: string[];
  tooltipLabels: string[];
  datasets: ReturnType<typeof buildTimeSeriesFromSummary>["datasets"];
  tooltipAfterBody?: (dataIndex: number) => string[];
  height?: number;
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
          ...(tooltipAfterBody
            ? {
                afterBody: (items) =>
                  tooltipAfterBody(items[0]?.dataIndex ?? 0),
              }
            : {}),
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
    <div style={{ position: "relative", height }}>
      <Line data={{ labels, datasets }} options={options} />
    </div>
  );
}

function ServerUsageTimeSeries({
  timeSeries,
  from,
  to,
  serverNameMappings,
  expanded = false,
}: {
  timeSeries: HooksTimeSeriesPoint[];
  from: Date;
  to: Date;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  expanded?: boolean;
}) {
  const timeRangeMs = to.getTime() - from.getTime();
  const { labels, tooltipLabels, datasets } = useMemo(
    () =>
      buildTimeSeriesFromSummary(
        timeSeries,
        (pt) => {
          return pt.serverName === "local"
            ? (serverNameMappings.rawToDisplay.get("") ?? "Local Tools")
            : (serverNameMappings.rawToDisplay.get(pt.serverName) ??
                pt.serverName);
        },
        timeRangeMs,
      ),
    [timeSeries, timeRangeMs, serverNameMappings.rawToDisplay],
  );
  return (
    <MultiLineChart
      labels={labels}
      tooltipLabels={tooltipLabels}
      datasets={datasets}
      height={expanded ? 500 : 200}
    />
  );
}

function UserUsageTimeSeries({
  timeSeries,
  from,
  to,
  expanded = false,
}: {
  timeSeries: HooksTimeSeriesPoint[];
  from: Date;
  to: Date;
  expanded?: boolean;
}) {
  const timeRangeMs = to.getTime() - from.getTime();
  const { labels, tooltipLabels, datasets } = useMemo(
    () =>
      buildTimeSeriesFromSummary(timeSeries, (pt) => pt.userEmail, timeRangeMs),
    [timeSeries, timeRangeMs],
  );
  return (
    <MultiLineChart
      labels={labels}
      tooltipLabels={tooltipLabels}
      datasets={datasets}
      height={expanded ? 500 : 200}
    />
  );
}

function ErrorsOverTimeChart({
  timeSeries,
  from,
  to,
  serverNameMappings,
  expanded = false,
}: {
  timeSeries: HooksTimeSeriesPoint[];
  from: Date;
  to: Date;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  expanded?: boolean;
}) {
  const timeRangeMs = to.getTime() - from.getTime();
  const { labels, tooltipLabels, datasets, hasErrors, perServerByIndex } =
    useMemo(() => {
      const built = buildTimeSeriesFromSummary(
        timeSeries,
        () => "errors",
        timeRangeMs,
        (pt) => pt.failureCount,
      );
      const errorColor = "#ef4444";
      const recoloredDatasets = built.datasets.map((ds) => ({
        ...ds,
        label: "Errors",
        borderColor: errorColor,
        backgroundColor: errorColor + "1a",
        pointBackgroundColor: errorColor,
      }));
      const total = built.datasets[0]?.data.reduce((s, n) => s + n, 0) ?? 0;

      // Build per-server error breakdown indexed by sorted timestamp position,
      // using the same BigInt conversion as buildTimeSeriesFromSummary.
      const allTimestamps = new Set<number>();
      for (const pt of timeSeries) {
        allTimestamps.add(Number(BigInt(pt.bucketStartNs) / BigInt(1_000_000)));
      }
      const sortedTs = Array.from(allTimestamps).sort((a, b) => a - b);
      const tsIndex = new Map<number, number>(
        sortedTs.map((ts, i): [number, number] => [ts, i]),
      );

      const accumulator = new Map<number, Map<string, number>>(
        sortedTs.map((_, i): [number, Map<string, number>] => [
          i,
          new Map<string, number>(),
        ]),
      );

      for (const pt of timeSeries) {
        if (pt.failureCount === 0) continue;
        const ms = Number(BigInt(pt.bucketStartNs) / BigInt(1_000_000));
        const idx = tsIndex.get(ms);
        if (idx === undefined) continue;
        const displayName =
          pt.serverName === "local"
            ? (serverNameMappings.rawToDisplay.get("") ?? "Local Tools")
            : (serverNameMappings.rawToDisplay.get(pt.serverName) ??
              pt.serverName);
        const map = accumulator.get(idx)!;
        map.set(displayName, (map.get(displayName) ?? 0) + pt.failureCount);
      }

      const perServerByIndex: { name: string; count: number }[][] = [];
      for (const [i, map] of accumulator) {
        perServerByIndex[i] = Array.from(map.entries())
          .filter(([, count]) => count > 0)
          .map(([name, count]) => ({ name, count }))
          .sort((a, b) => b.count - a.count);
      }

      return {
        labels: built.labels,
        tooltipLabels: built.tooltipLabels,
        datasets: recoloredDatasets,
        hasErrors: total > 0,
        perServerByIndex,
      };
    }, [timeSeries, timeRangeMs, serverNameMappings.rawToDisplay]);

  if (!hasErrors) {
    return (
      <div className="text-muted-foreground flex h-[200px] items-center justify-center text-sm">
        No errors in this period
      </div>
    );
  }

  return (
    <MultiLineChart
      labels={labels}
      tooltipLabels={tooltipLabels}
      datasets={datasets}
      height={expanded ? 500 : 200}
      tooltipAfterBody={(idx) => {
        const servers = perServerByIndex[idx];
        if (!servers || servers.length === 0) return [];
        return servers.map((s) => `${s.name}: ${s.count}`);
      }}
    />
  );
}

function HooksAnalytics({
  serverNameMappings,
  from,
  to,
  compact = false,
  addFilter,
  onHookTypesChange,
  summaryData,
  summaryPending,
  summaryIsError,
}: {
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  from: Date;
  to: Date;
  compact?: boolean;
  addFilter: (chip: FilterChip) => void;
  onHookTypesChange: (types: TypesToInclude[]) => void;
  summaryData: GetHooksSummaryResult | undefined;
  summaryPending: boolean;
  summaryIsError: boolean;
}) {
  const breakdown = summaryData?.breakdown ?? [];
  const timeSeries = summaryData?.timeSeries ?? [];

  const kpis = useMemo(() => {
    if (!summaryData) return null;
    const { servers, users, breakdown: bd } = summaryData;

    const totalEvents = summaryData.totalEvents;
    const totalSuccesses = servers.reduce((s, r) => s + r.successCount, 0);
    const totalFailures = servers.reduce((s, r) => s + r.failureCount, 0);
    const completedEvents = totalSuccesses + totalFailures;
    const avgSuccessRate =
      completedEvents > 0 ? (totalSuccesses / completedEvents) * 100 : null;

    const activeUsers = users.length;
    const activeSources = new Set(bd.map((r) => r.hookSource).filter(Boolean))
      .size;
    const uniqueTools = servers.reduce((s, r) => s + r.uniqueTools, 0);

    return {
      avgSuccessRate,
      totalEvents,
      activeUsers,
      activeSources,
      uniqueTools,
    };
  }, [summaryData]);

  const hasServers = (summaryData?.servers.length ?? 0) > 0;
  const [expandedChart, setExpandedChart] = useState<string | null>(null);

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
        {summaryIsError && !summaryData ? (
          <div className="col-span-full">
            <ErrorAlert
              error={new Error("Failed to load analytics summary")}
              title="Error loading analytics"
            />
          </div>
        ) : summaryPending || !summaryData ? (
          <>
            {Array.from({ length: compact ? 3 : 5 }).map((_, i) => (
              <Skeleton key={i} className="h-[104px] rounded-lg" />
            ))}
          </>
        ) : (
          <>
            <MetricCard
              title="Avg Success Rate"
              value={kpis?.avgSuccessRate ?? 0}
              format="percent"
              icon="circle-check"
              accentColor="green"
            />
            <MetricCard
              title="Total Events"
              value={kpis?.totalEvents ?? 0}
              icon="activity"
              accentColor="purple"
            />
            <MetricCard
              title="Active Users"
              value={kpis?.activeUsers ?? 0}
              icon="users"
              accentColor="yellow"
            />
            <MetricCard
              title="Active Sources"
              value={kpis?.activeSources ?? 0}
              icon="monitor"
              accentColor="blue"
            />
            <MetricCard
              title="Unique Tools"
              value={kpis?.uniqueTools ?? 0}
              icon="wrench"
              accentColor="orange"
            />
          </>
        )}
      </div>

      {(timeSeries.length > 0 || hasServers) && (
        <div
          className={cn(
            "grid gap-4",
            expandedChart
              ? "grid-cols-1"
              : compact
                ? "grid-cols-1"
                : "grid-cols-1 lg:grid-cols-2",
          )}
        >
          {timeSeries.length > 0 &&
            (!expandedChart || expandedChart === "server-usage") && (
              <ChartCard
                title="Server Usage"
                chartId="server-usage"
                expandedChart={expandedChart}
                onExpand={setExpandedChart}
              >
                <ServerUsageTimeSeries
                  timeSeries={timeSeries}
                  from={from}
                  to={to}
                  serverNameMappings={serverNameMappings}
                  expanded={expandedChart === "server-usage"}
                />
              </ChartCard>
            )}
          {hasServers &&
            (!expandedChart || expandedChart === "users-per-server") && (
              <UsersPerServerChart
                title="Users per Server"
                breakdown={breakdown}
                serverNameMappings={serverNameMappings}
                handleFilter={makeFilterHandler({
                  server: "row",
                  user: "dataset",
                })}
                expandedChart={expandedChart}
                onExpand={setExpandedChart}
              />
            )}

          {timeSeries.length > 0 &&
            (!expandedChart || expandedChart === "user-usage") && (
              <ChartCard
                title="User Usage"
                chartId="user-usage"
                expandedChart={expandedChart}
                onExpand={setExpandedChart}
              >
                <UserUsageTimeSeries
                  timeSeries={timeSeries}
                  from={from}
                  to={to}
                  expanded={expandedChart === "user-usage"}
                />
              </ChartCard>
            )}
          {hasServers &&
            (!expandedChart || expandedChart === "user-event-counts") && (
              <UserEventCountsChart
                title="User Event Counts"
                breakdown={breakdown}
                handleFilter={makeFilterHandler({ user: "row" })}
                expandedChart={expandedChart}
                onExpand={setExpandedChart}
              />
            )}

          {timeSeries.length > 0 &&
            (!expandedChart || expandedChart === "errors-over-time") && (
              <ChartCard
                title="Errors Over Time"
                chartId="errors-over-time"
                expandedChart={expandedChart}
                onExpand={setExpandedChart}
              >
                <ErrorsOverTimeChart
                  timeSeries={timeSeries}
                  from={from}
                  to={to}
                  serverNameMappings={serverNameMappings}
                  expanded={expandedChart === "errors-over-time"}
                />
              </ChartCard>
            )}
          {hasServers &&
            (!expandedChart || expandedChart === "errors-per-server") && (
              <ServerErrorRateChart
                title="Errors per Server and Tool"
                breakdown={breakdown}
                serverNameMappings={serverNameMappings}
                expandedChart={expandedChart}
                onExpand={setExpandedChart}
              />
            )}
        </div>
      )}
    </div>
  );
}
