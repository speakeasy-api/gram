import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { InsightsConfig } from "@/components/insights-sidebar";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { ErrorAlert } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import {
  FilterChip,
  ObserveFilterBar,
  type ObserveTypeFilterValue,
} from "@/components/observe/ObserveFilterBar";
import { useSlugs } from "@/contexts/Sdk";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { useServerNameMappings } from "@/hooks/useServerNameMappings";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { getPresetRange, type DateRangePreset } from "@gram-ai/elements";
import { telemetryGetToolUsageSummary } from "@gram/client/funcs/telemetryGetToolUsageSummary";
import type {
  GetToolUsageSummaryResult,
  ToolUsageTargetSummary,
  ToolUsageTargetTimeSeriesPoint,
  ToolUsageTargetToolBreakdownRow,
  ToolUsageUserSummary,
  ToolUsageUserTimeSeriesPoint,
  ToolUsageUsersByTargetRow,
} from "@gram/client/models/components";
import type { TargetTypes } from "@gram/client/models/components/gettoolusagesummarypayload";
import { useGramContext } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import { Badge, Icon } from "@speakeasy-api/moonshine";
import { ChartCard } from "@/components/chart/ChartCard";
import { MetricCard } from "@/components/chart/MetricCard";
import { formatChartLabel } from "@/components/chart/chartUtils";
import { useExpandedChart } from "@/hooks/useExpandedChart";
import { useQuery } from "@tanstack/react-query";
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
import { Settings } from "lucide-react";
import { useCallback, useEffect, useMemo } from "react";
import { Link } from "react-router";
import { useObserveFilters } from "@/components/observe/useObserveFilters";
import { HooksEmptyState } from "@/pages/hooks/HooksEmptyState";
import { HooksSetupButton } from "@/pages/hooks/HooksSetupDialog";
import type { MultiSelectGroup } from "@/components/ui/multi-select";

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

const CHART_COLORS = {
  label: "#737373",
  labelFaded: "#A3A3A3",
  gridLine: "#e5e5e5",
  tooltipBg: "#171717",
  tooltipTitle: "#fafafa",
  tooltipBody: "#d4d4d4",
  tooltipBorder: "#262626",
} as const;

const USER_SOURCE_COLORS = [
  "#60a5fa",
  "#fb923c",
  "#34d399",
  "#f87171",
  "#a78bfa",
  "#facc15",
  "#22d3ee",
  "#f472b6",
  "#a3e635",
];

const BRAND_RED_COLORS = [
  "#fb923c",
  "#ea580c",
  "#dc2626",
  "#b91c1c",
  "#991b1b",
  "#7f1d1d",
];

const COLLAPSED_BAR_CHART_MAX_ROWS = 6;
const BAR_THICKNESS = { collapsed: 18, expanded: 24 };
const BAR_ROW_HEIGHT = { collapsed: 18, expanded: 24 };
const BAR_ROW_SPACER = { collapsed: 8, expanded: 12 };
const LINE_CHART_HEIGHT = { collapsed: 250, expanded: 600 };
const HOSTED_SERVER_PREFIX = "hosted:";
const SHADOW_SERVER_PREFIX = "shadow:";

const TOOL_USAGE_DEFAULT_TYPES: ObserveTypeFilterValue[] = [];
const TOOL_USAGE_VALID_TYPES: ObserveTypeFilterValue[] = [
  "hosted_mcp_server",
  "shadow_mcp_server",
  "local_tool",
  "skill",
];
const TOOL_USAGE_TYPE_OPTIONS: Array<{
  label: string;
  value: ObserveTypeFilterValue;
}> = [
  { label: "Hosted MCP Servers", value: "hosted_mcp_server" },
  { label: "Shadow MCP Servers", value: "shadow_mcp_server" },
  { label: "Local Tools", value: "local_tool" },
  { label: "Skills", value: "skill" },
];

const SERVER_FILTER_PATH = "gram.tool_call.source";
const USER_EMAIL_FILTER_PATH = "user.email";

type ParsedServerFilter =
  | { type: "hosted"; id: string }
  | { type: "shadow"; id: string };

function encodeHostedServerFilter(slug: string): string {
  return `${HOSTED_SERVER_PREFIX}${slug}`;
}

function encodeShadowServerFilter(name: string): string {
  return name;
}

function parseServerFilter(value: string): ParsedServerFilter {
  if (value.startsWith(HOSTED_SERVER_PREFIX)) {
    return { type: "hosted", id: value.slice(HOSTED_SERVER_PREFIX.length) };
  }
  if (value.startsWith(SHADOW_SERVER_PREFIX)) {
    return { type: "shadow", id: value.slice(SHADOW_SERVER_PREFIX.length) };
  }
  return { type: "shadow", id: value };
}

function selectedServerValues(activeFilters: FilterChip[]): string[] {
  return activeFilters
    .filter((f) => f.path === SERVER_FILTER_PATH)
    .flatMap((f) => f.filters)
    .filter(Boolean);
}

function selectedUserEmails(activeFilters: FilterChip[]): string[] {
  return activeFilters
    .filter((f) => f.path === USER_EMAIL_FILTER_PATH)
    .flatMap((f) => f.filters)
    .filter(Boolean);
}

function toTargetTypes(
  selectedTypes: ObserveTypeFilterValue[],
): TargetTypes[] | undefined {
  const mapped = selectedTypes.filter((type): type is TargetTypes =>
    TOOL_USAGE_VALID_TYPES.includes(type),
  );
  return mapped.length > 0 ? mapped : undefined;
}

function isServerTarget(target: ToolUsageTargetSummary): boolean {
  return (
    target.targetType === "hosted_mcp_server" ||
    target.targetType === "shadow_mcp_server"
  );
}

function displayTargetLabel(
  targetLabel: string,
  targetType: string,
  serverNameMappings: ReturnType<typeof useServerNameMappings>,
): string {
  if (targetType === "local_tool") return "Local Tools";
  if (targetType === "shadow_mcp_server") {
    return serverNameMappings.rawToDisplay.get(targetLabel) ?? targetLabel;
  }
  return targetLabel;
}

function buildServerOptionGroups({
  summary,
  activeFilters,
  serverNameMappings,
}: {
  summary: GetToolUsageSummaryResult | undefined;
  activeFilters: FilterChip[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
}): MultiSelectGroup[] {
  const hosted = new Map<string, { label: string; count: number }>();
  const shadow = new Map<string, { label: string; count: number }>();

  for (const target of summary?.targets ?? []) {
    if (!isServerTarget(target)) continue;
    const label = displayTargetLabel(
      target.targetLabel,
      target.targetType,
      serverNameMappings,
    );
    if (target.targetType === "hosted_mcp_server") {
      hosted.set(encodeHostedServerFilter(target.targetId), {
        label,
        count: target.eventCount,
      });
    } else {
      shadow.set(encodeShadowServerFilter(target.targetId), {
        label,
        count: target.eventCount,
      });
    }
  }

  for (const value of selectedServerValues(activeFilters)) {
    const parsed = parseServerFilter(value);
    if (parsed.type === "hosted" && !hosted.has(value)) {
      hosted.set(value, { label: parsed.id, count: 0 });
    }
    const encodedShadow = encodeShadowServerFilter(parsed.id);
    if (parsed.type === "shadow" && !shadow.has(encodedShadow)) {
      shadow.set(encodedShadow, {
        label: serverNameMappings.rawToDisplay.get(parsed.id) ?? parsed.id,
        count: 0,
      });
    }
  }

  const toOptions = (entries: Map<string, { label: string; count: number }>) =>
    Array.from(entries.entries())
      .sort(
        (a, b) =>
          b[1].count - a[1].count || a[1].label.localeCompare(b[1].label),
      )
      .map(([value, option]) => ({
        value,
        label:
          option.count > 0 ? `${option.label} (${option.count})` : option.label,
      }));

  return [
    { heading: "Hosted MCP", options: toOptions(hosted) },
    { heading: "Shadow MCP", options: toOptions(shadow) },
  ].filter((group) => group.options.length > 0);
}

type _BarLegend = Exclude<
  NonNullable<ChartOptions<"bar">["plugins"]>["legend"],
  false
>;
type _BarTooltip = NonNullable<ChartOptions<"bar">["plugins"]>["tooltip"];
type _BarScales = NonNullable<ChartOptions<"bar">["scales"]>;

const SHARED_RESIZE_TRANSITION = {
  resize: { animation: { duration: 0 } },
} as const;

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

export function InsightsToolsContent(): JSX.Element {
  const { projectSlug } = useSlugs();

  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: ({ toolName }) =>
      toolName.includes("logs") || toolName.includes("hooks"),
  });

  const serverNameMappings = useServerNameMappings();

  const {
    from,
    to,
    selectedHookTypes,
    activeFilters,
    serverOptions,
    handleServerSelectionChange,
    handleUserEmailSelectionChange,
    hookSourceOptions,
    handleHookSourceSelectionChange,
    addFilter,
    handleHookTypesChange,
    dateRange,
    customRange,
    customRangeLabel,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
    selectedRoleIds,
    roleOptions,
    handleRoleSelectionChange,
    roleFilterPending,
    roleEmails,
  } = useObserveFilters({
    defaultTypes: TOOL_USAGE_DEFAULT_TYPES,
    validTypes: TOOL_USAGE_VALID_TYPES,
  });

  const client = useGramContext();

  const serverFilters = useMemo(
    () => selectedServerValues(activeFilters).map(parseServerFilter),
    [activeFilters],
  );
  const hostedToolsetSlugs = useMemo(
    () =>
      serverFilters
        .filter((filter) => filter.type === "hosted")
        .map((filter) => filter.id),
    [serverFilters],
  );
  const shadowServerNames = useMemo(
    () =>
      serverFilters
        .filter((filter) => filter.type === "shadow")
        .map((filter) => filter.id),
    [serverFilters],
  );
  const userFilters = useMemo(() => {
    const emails = [
      ...new Set([...selectedUserEmails(activeFilters), ...roleEmails]),
    ];
    return emails.map((email) => ({ kind: "email" as const, key: email }));
  }, [activeFilters, roleEmails]);

  const {
    data: summaryData,
    error,
    isFetching: summaryFetching,
    isPending: summaryPending,
    isError: summaryIsError,
    refetch: refetchSummary,
    isLogsDisabled: isLogsLogsDisabled,
  } = useLogsEnabledErrorCheck(
    useQuery({
      queryKey: [
        "tool-usage-summary",
        from.toISOString(),
        to.toISOString(),
        hostedToolsetSlugs,
        shadowServerNames,
        userFilters,
        selectedHookTypes,
      ],
      queryFn: () =>
        unwrapAsync(
          telemetryGetToolUsageSummary(client, {
            getToolUsageSummaryPayload: {
              from,
              to,
              hostedToolsetSlugs:
                hostedToolsetSlugs.length > 0 ? hostedToolsetSlugs : undefined,
              shadowServerNames:
                shadowServerNames.length > 0 ? shadowServerNames : undefined,
              targetTypes: toTargetTypes(selectedHookTypes),
              userFilters: userFilters.length > 0 ? userFilters : undefined,
            },
          }),
        ),
      enabled: !roleFilterPending,
      throwOnError: false,
    }),
  );

  const { data: filterOptionsSummary, isPending: filterOptionsPending } =
    useQuery({
      queryKey: [
        "tool-usage-filter-options",
        from.toISOString(),
        to.toISOString(),
      ],
      queryFn: () =>
        unwrapAsync(
          telemetryGetToolUsageSummary(client, {
            getToolUsageSummaryPayload: {
              from,
              to,
            },
          }),
        ),
      throwOnError: false,
    });

  const serverOptionGroups = useMemo(
    () =>
      buildServerOptionGroups({
        summary: filterOptionsSummary,
        activeFilters,
        serverNameMappings,
      }),
    [activeFilters, filterOptionsSummary, serverNameMappings],
  );

  const toolUsageUserEmailOptions = useMemo(() => {
    const selected = selectedUserEmails(activeFilters);
    const known = (filterOptionsSummary?.users ?? [])
      .filter((user) => user.userKind === "email")
      .map((user) => user.userKey || user.userLabel)
      .filter(Boolean);
    return [...new Set([...known, ...selected])];
  }, [activeFilters, filterOptionsSummary]);

  const refetch = useCallback(() => {
    void refetchSummary();
  }, [refetchSummary]);

  const isLogsDisabled = isLogsLogsDisabled;
  const isLoading = summaryFetching && !summaryData;

  return (
    <>
      <InsightsConfig
        mcpConfig={mcpConfig}
        title="Explore Tools"
        subtitle="Ask me about your tools! Powered by Elements + platform MCP"
        hideTrigger={isLogsDisabled}
      />
      {isLogsDisabled ? (
        <div className="min-h-0 w-full flex-1 space-y-6 overflow-y-auto p-8 pb-24">
          <div className="flex min-w-0 flex-col gap-1">
            <h1 className="text-xl font-semibold">Tools</h1>
            <p className="text-muted-foreground text-sm">
              Monitor tool events across all users and agents in your project
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
        </div>
      ) : (
        <HooksInnerContent
          isLogsDisabled={isLogsDisabled}
          isLoading={isLoading}
          error={error}
          serverOptions={serverOptions}
          serverOptionGroups={serverOptionGroups}
          onServerSelectionChange={handleServerSelectionChange}
          userEmailOptions={toolUsageUserEmailOptions}
          onUserEmailSelectionChange={handleUserEmailSelectionChange}
          sourceOptions={hookSourceOptions}
          onSourceSelectionChange={handleHookSourceSelectionChange}
          activeFilters={activeFilters}
          addFilter={addFilter}
          selectedHookTypes={selectedHookTypes}
          onHookTypesChange={handleHookTypesChange}
          typeOptions={TOOL_USAGE_TYPE_OPTIONS}
          roleOptions={roleOptions}
          selectedRoleIds={selectedRoleIds}
          onRoleSelectionChange={handleRoleSelectionChange}
          dateRange={dateRange}
          customRange={customRange}
          customRangeLabel={customRangeLabel}
          onDateRangeChange={setDateRangeParam}
          onCustomRangeChange={setCustomRangeParam}
          onClearCustomRange={clearCustomRange}
          projectSlug={projectSlug}
          serverNameMappings={serverNameMappings}
          summaryData={summaryData}
          summaryPending={summaryPending || filterOptionsPending}
          summaryIsError={summaryIsError}
        />
      )}
    </>
  );
}

function HooksInnerContent({
  isLoading,
  error,
  serverOptions,
  serverOptionGroups,
  onServerSelectionChange,
  userEmailOptions,
  onUserEmailSelectionChange,
  sourceOptions,
  onSourceSelectionChange,
  activeFilters,
  addFilter,
  selectedHookTypes,
  onHookTypesChange,
  typeOptions,
  roleOptions,
  selectedRoleIds,
  onRoleSelectionChange,
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
  error: Error | null;
  serverOptions: string[];
  serverOptionGroups: MultiSelectGroup[];
  onServerSelectionChange: (values: string[]) => void;
  userEmailOptions: string[];
  onUserEmailSelectionChange: (values: string[]) => void;
  sourceOptions: string[];
  onSourceSelectionChange: (values: string[]) => void;
  activeFilters: FilterChip[];
  addFilter: (chip: FilterChip) => void;
  selectedHookTypes: ObserveTypeFilterValue[];
  onHookTypesChange: (types: ObserveTypeFilterValue[]) => void;
  typeOptions: Array<{ label: string; value: ObserveTypeFilterValue }>;
  roleOptions: Array<{ id: string; name: string }>;
  selectedRoleIds: string[];
  onRoleSelectionChange: (values: string[]) => void;
  dateRange: DateRangePreset;
  customRange: { from: Date; to: Date } | null;
  customRangeLabel: string | null;
  onDateRangeChange: (preset: DateRangePreset) => void;
  onCustomRangeChange: (from: Date, to: Date, label?: string) => void;
  onClearCustomRange: () => void;
  projectSlug?: string;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  summaryData: GetToolUsageSummaryResult | undefined;
  summaryPending: boolean;
  summaryIsError: boolean;
}) {
  const orgRoutes = useOrgRoutes();
  const { from, to } = useMemo(
    () => customRange ?? getPresetRange(dateRange),
    [customRange, dateRange],
  );
  const { expandedChart, setExpandedChart } = useExpandedChart();
  useEffect(() => {
    if (summaryPending) setExpandedChart(null);
  }, [summaryPending, setExpandedChart]);
  const hasSummaryData = (summaryData?.totals.eventCount ?? 0) > 0;

  return (
    <div className="flex min-h-0 w-full flex-1 flex-col">
      <div className="flex min-h-0 flex-1 flex-col gap-6 px-8 pt-8">
        <div className="flex shrink-0 items-start justify-between gap-4">
          <div className="flex min-w-0 flex-col gap-1">
            <h1 className="text-xl font-semibold">Tools</h1>
            <p className="text-muted-foreground text-sm">
              Monitor tool events across all users and agents in your project
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

        <ObserveFilterBar
          serverOptions={serverOptions}
          serverOptionGroups={serverOptionGroups}
          onServerSelectionChange={onServerSelectionChange}
          userEmailOptions={userEmailOptions}
          onUserEmailSelectionChange={onUserEmailSelectionChange}
          sourceOptions={sourceOptions}
          onSourceSelectionChange={onSourceSelectionChange}
          activeFilters={activeFilters}
          selectedTypes={selectedHookTypes}
          onTypesChange={onHookTypesChange}
          typeOptions={typeOptions}
          roleOptions={roleOptions}
          selectedRoleIds={selectedRoleIds}
          onRoleSelectionChange={onRoleSelectionChange}
          dateRange={dateRange}
          customRange={customRange}
          customRangeLabel={customRangeLabel}
          onDateRangeChange={onDateRangeChange}
          onCustomRangeChange={onCustomRangeChange}
          onClearCustomRange={onClearCustomRange}
          projectSlug={projectSlug}
          serverNameMappings={serverNameMappings}
        />

        <div className="flex min-h-0 flex-1 overflow-hidden">
          <div className="min-h-0 flex-1 overflow-y-auto pb-4">
            {error ? (
              <ErrorAlert
                error={error}
                title="Error loading tool usage"
                className="mx-auto w-full"
              />
            ) : isLoading ? (
              <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
                <Spinner className="mr-0 size-5" />
                <span>Loading tool usage...</span>
              </div>
            ) : !hasSummaryData &&
              activeFilters.length === 0 &&
              selectedRoleIds.length === 0 ? (
              <HooksEmptyState
                title="No Insights Generated"
                subtitle="Install Observability plugin in your AI agent to start generating tool insights"
              />
            ) : !hasSummaryData ? (
              <div className="py-12 text-center">
                <div className="flex flex-col items-center gap-3">
                  <div className="bg-muted flex size-12 items-center justify-center rounded-full">
                    <Icon
                      name="inbox"
                      className="text-muted-foreground size-6"
                    />
                  </div>
                  <span className="text-foreground font-medium">
                    No matching tool usage
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
                compact={false}
                addFilter={addFilter}
                onHookTypesChange={onHookTypesChange}
                summaryData={summaryData}
                summaryPending={summaryPending}
                summaryIsError={summaryIsError}
                expandedChart={expandedChart}
                onExpandedChartChange={setExpandedChart}
              />
            )}
          </div>
        </div>
      </div>
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
  tooltipLabelFn,
  expanded = false,
  maxRows,
  onShowAll,
}: {
  labels: string[];
  datasets: StackedBarDataset[];
  handleFilter?: (datasetLabel: string, rowLabel: string) => void;
  tooltipLabelFn?: (item: TooltipItem<"bar">) => string | string[] | undefined;
  expanded?: boolean;
  maxRows?: number;
  onShowAll?: () => void;
}) {
  const thickness = expanded ? BAR_THICKNESS.expanded : BAR_THICKNESS.collapsed;
  const hiddenCount =
    !expanded && maxRows && labels.length > maxRows
      ? labels.length - maxRows
      : 0;
  const visibleLabels = hiddenCount > 0 ? labels.slice(0, maxRows) : labels;
  const visibleDatasets = (
    hiddenCount > 0
      ? datasets.map((ds) => ({ ...ds, data: ds.data.slice(0, maxRows) }))
      : datasets
  ).map((ds) => ({ ...ds, barThickness: thickness }));

  const rowH = expanded ? BAR_ROW_HEIGHT.expanded : BAR_ROW_HEIGHT.collapsed;
  const rowS = expanded ? BAR_ROW_SPACER.expanded : BAR_ROW_SPACER.collapsed;
  const containerHeight = Math.max(
    120,
    visibleLabels.length * (rowH + rowS) + 60,
  );

  const options = useMemo<ChartOptions<"bar">>(
    () => ({
      indexAxis: "y",
      responsive: true,
      maintainAspectRatio: false,
      onClick(_, elements) {
        if (!elements.length || !handleFilter) return;
        const { datasetIndex, index } = elements[0]!;
        const datasetLabel = datasets[datasetIndex]?.label;
        const rowLabel = visibleLabels[index];
        if (datasetLabel && rowLabel) handleFilter(datasetLabel, rowLabel);
      },
      onHover(event, elements) {
        const el = event.native?.target as HTMLElement | null;
        if (el) el.style.cursor = elements.length ? "pointer" : "default";
      },
      scales: SHARED_BAR_SCALES,
      transitions: SHARED_RESIZE_TRANSITION,
      plugins: {
        legend: SHARED_LEGEND,
        tooltip: {
          ...SHARED_TOOLTIP,
          callbacks: {
            label:
              tooltipLabelFn ??
              ((item: TooltipItem<"bar">) =>
                ` ${item.dataset.label}: ${item.parsed.x}`),
          },
        },
      },
    }),
    [datasets, visibleLabels, handleFilter, tooltipLabelFn],
  );

  if (visibleLabels.length === 0) return null;

  return (
    <>
      <div
        className="transition-all duration-200 ease-in-out"
        style={{ height: containerHeight }}
      >
        <Bar
          plugins={STACKED_BAR_PLUGINS}
          data={{ labels: visibleLabels, datasets: visibleDatasets }}
          options={options}
        />
      </div>
      {hiddenCount > 0 && onShowAll && (
        <div className="mt-2 flex w-full">
          <Button
            variant="ghost"
            size="sm"
            icon="chevron-down"
            iconAfter={true}
            onClick={onShowAll}
          >
            Show {hiddenCount} more
          </Button>
        </div>
      )}
    </>
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
  breakdown: ToolUsageUsersByTargetRow[];
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
      const user = row.userLabel || "unknown";
      const displayName = displayTargetLabel(
        row.targetLabel,
        row.targetType,
        serverNameMappings,
      );
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
      backgroundColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length]!,
      hoverBackgroundColor:
        USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length]! + "cc",
    }));

    return { labels: chartLabels, datasets: chartDatasets };
  }, [breakdown, serverNameMappings]);

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={labels.length > 0}
    >
      {labels.length === 0 ? (
        <ChartNoData />
      ) : (
        <StackedBarChart
          labels={labels}
          datasets={datasets}
          handleFilter={handleFilter}
          expanded={expanded}
          maxRows={COLLAPSED_BAR_CHART_MAX_ROWS}
          onShowAll={() => onExpand(chartId)}
        />
      )}
    </ChartCard>
  );
}

function UserEventCountsChart({
  title,
  users,
  handleFilter,
  expandedChart,
  onExpand,
}: {
  title: string;
  users: ToolUsageUserSummary[];
  handleFilter?: (datasetLabel: string, userEmail: string) => void;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const chartId = "user-event-counts";
  const expanded = expandedChart === chartId;
  const { labels, datasets } = useMemo(() => {
    const sortedUsers = [...users].sort((a, b) => b.eventCount - a.eventCount);

    const chartLabels = sortedUsers.map((user) => user.userLabel || "unknown");
    const color = USER_SOURCE_COLORS[0]!;
    const chartDatasets = [
      {
        label: "Events",
        barThickness: 24,
        data: sortedUsers.map((user) => user.eventCount),
        backgroundColor: color,
        hoverBackgroundColor: color + "cc",
      },
    ];

    return { labels: chartLabels, datasets: chartDatasets };
  }, [users]);

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={labels.length > 0}
    >
      {labels.length === 0 ? (
        <ChartNoData />
      ) : (
        <StackedBarChart
          labels={labels}
          datasets={datasets}
          handleFilter={handleFilter}
          expanded={expanded}
          maxRows={COLLAPSED_BAR_CHART_MAX_ROWS}
          onShowAll={() => onExpand(chartId)}
        />
      )}
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
  breakdown: ToolUsageTargetToolBreakdownRow[];
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const chartId = "errors-per-server";
  const expanded = expandedChart === chartId;
  const { labels, datasets } = useMemo(() => {
    const serverMap = new Map<string, Map<string, number>>();
    const toolSet = new Set<string>();
    for (const row of breakdown) {
      if (row.failureCount === 0) continue;
      const displayName = displayTargetLabel(
        row.targetLabel,
        row.targetType,
        serverNameMappings,
      );
      const tool = row.toolName || "unknown";
      toolSet.add(tool);
      const inner = serverMap.get(displayName) ?? new Map<string, number>();
      inner.set(tool, (inner.get(tool) ?? 0) + row.failureCount);
      serverMap.set(displayName, inner);
    }

    const sortedServers = Array.from(serverMap.entries())
      .map(([displayName, toolCounts]) => ({
        displayName,
        total: Array.from(toolCounts.values()).reduce((a, b) => a + b, 0),
        toolCounts,
      }))
      .sort((a, b) => b.total - a.total);

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
      barThickness: BAR_THICKNESS.collapsed,
      data: sortedServers.map((s) => s.toolCounts.get(tool) ?? 0),
      backgroundColor: BRAND_RED_COLORS[i % BRAND_RED_COLORS.length]!,
      hoverBackgroundColor:
        BRAND_RED_COLORS[i % BRAND_RED_COLORS.length]! + "cc",
    }));

    return { labels: chartLabels, datasets: chartDatasets };
  }, [breakdown, serverNameMappings]);

  const hiddenCount =
    !expanded && labels.length > COLLAPSED_BAR_CHART_MAX_ROWS
      ? labels.length - COLLAPSED_BAR_CHART_MAX_ROWS
      : 0;
  const visibleLabels =
    hiddenCount > 0 ? labels.slice(0, COLLAPSED_BAR_CHART_MAX_ROWS) : labels;
  const thickness = expanded ? BAR_THICKNESS.expanded : BAR_THICKNESS.collapsed;
  const visibleDatasets = (
    hiddenCount > 0
      ? datasets.map((ds) => ({
          ...ds,
          data: ds.data.slice(0, COLLAPSED_BAR_CHART_MAX_ROWS),
        }))
      : datasets
  ).map((ds) => ({ ...ds, barThickness: thickness }));

  const rowH = expanded ? BAR_ROW_HEIGHT.expanded : BAR_ROW_HEIGHT.collapsed;
  const rowS = expanded ? BAR_ROW_SPACER.expanded : BAR_ROW_SPACER.collapsed;
  const height = Math.max(120, visibleLabels.length * (rowH + rowS) + 60);

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
    transitions: SHARED_RESIZE_TRANSITION,
  };

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={labels.length > 0}
    >
      {labels.length === 0 ? (
        <ChartNoData message="No errors in this period" />
      ) : (
        <>
          <div
            className="relative transition-all duration-200 ease-in-out"
            style={{ height }}
          >
            <Bar
              data={{ labels: visibleLabels, datasets: visibleDatasets }}
              options={options}
            />
          </div>
          {hiddenCount > 0 && (
            <button
              type="button"
              onClick={() => onExpand(chartId)}
              className="text-muted-foreground hover:text-foreground mt-1 w-full text-center text-xs underline-offset-2 hover:underline"
            >
              Show {hiddenCount} more
            </button>
          )}
        </>
      )}
    </ChartCard>
  );
}

type TimeSeriesDataset = {
  label: string;
  data: number[];
  borderColor: string;
  backgroundColor: string;
  pointBackgroundColor: string;
  fill: boolean;
  tension: number;
  borderWidth: number;
  pointRadius: number;
  pointHoverRadius: number;
};

function buildTimeSeriesFromSummary<
  T extends { bucketStartNs: string; eventCount: number },
>(
  timeSeries: T[],
  keyFn: (p: T) => string,
  timeRangeMs: number,
  valueFn: (p: T) => number = (p) => p.eventCount,
) {
  if (timeSeries.length === 0)
    return { labels: [], tooltipLabels: [], datasets: [] };

  const seriesMap = new Map<string, Map<number, number>>();

  for (const pt of timeSeries) {
    const key = keyFn(pt);
    if (!key) continue;
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

  const datasets: TimeSeriesDataset[] = Array.from(seriesMap.entries()).map(
    ([key, series], i) => {
      const color = USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length]!;
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
    },
  );

  return { labels, tooltipLabels, datasets };
}

function ChartNoData({
  message = "No data in this period",
}: {
  message?: string;
}) {
  return (
    <div className="flex h-24 items-center justify-center">
      <Badge variant="neutral">
        <Badge.LeftIcon>
          <Icon name="chart-no-axes-column" size="small" />
        </Badge.LeftIcon>
        <Badge.Text>{message}</Badge.Text>
      </Badge>
    </div>
  );
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
  datasets: TimeSeriesDataset[];
  tooltipAfterBody?: (dataIndex: number) => string[];
  height?: number;
}) {
  if (labels.length === 0) {
    return <ChartNoData />;
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
          label: (item) => {
            if ((item.parsed.y ?? 0) === 0) return undefined;
            return item.formattedValue
              ? `${item.dataset.label}: ${item.formattedValue}`
              : "";
          },
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
    transitions: SHARED_RESIZE_TRANSITION,
  };

  return (
    <div
      className="relative transition-all duration-200 ease-in-out"
      style={{ height }}
    >
      <Line data={{ labels, datasets }} options={options} />
    </div>
  );
}

function ServerUsageTimeSeries({
  timeSeries,
  from,
  to,
  serverNameMappings,
  expandedChart,
  onExpand,
}: {
  timeSeries: ToolUsageTargetTimeSeriesPoint[];
  from: Date;
  to: Date;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const chartId = "server-usage";
  const expanded = expandedChart === chartId;
  const timeRangeMs = to.getTime() - from.getTime();
  const { labels, tooltipLabels, datasets } = useMemo(
    () =>
      buildTimeSeriesFromSummary(
        timeSeries,
        (pt) =>
          displayTargetLabel(pt.targetLabel, pt.targetType, serverNameMappings),
        timeRangeMs,
      ),
    [timeSeries, timeRangeMs, serverNameMappings],
  );
  return (
    <ChartCard
      title="Target Usage"
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={labels.length > 0}
    >
      <MultiLineChart
        labels={labels}
        tooltipLabels={tooltipLabels}
        datasets={datasets}
        height={
          expanded ? LINE_CHART_HEIGHT.expanded : LINE_CHART_HEIGHT.collapsed
        }
      />
    </ChartCard>
  );
}

function UserUsageTimeSeries({
  timeSeries,
  from,
  to,
  expandedChart,
  onExpand,
}: {
  timeSeries: ToolUsageUserTimeSeriesPoint[];
  from: Date;
  to: Date;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const chartId = "user-usage";
  const expanded = expandedChart === chartId;
  const timeRangeMs = to.getTime() - from.getTime();
  const { labels, tooltipLabels, datasets } = useMemo(
    () =>
      buildTimeSeriesFromSummary(timeSeries, (pt) => pt.userLabel, timeRangeMs),
    [timeSeries, timeRangeMs],
  );
  return (
    <ChartCard
      title="User Usage"
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={labels.length > 0}
    >
      <MultiLineChart
        labels={labels}
        tooltipLabels={tooltipLabels}
        datasets={datasets}
        height={
          expanded ? LINE_CHART_HEIGHT.expanded : LINE_CHART_HEIGHT.collapsed
        }
      />
    </ChartCard>
  );
}

function SkillUsageTimeSeries({
  skillTimeSeries,
  from,
  to,
  expandedChart,
  onExpand,
}: {
  skillTimeSeries: ToolUsageTargetTimeSeriesPoint[];
  from: Date;
  to: Date;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const chartId = "skill-usage";
  const expanded = expandedChart === chartId;
  const timeRangeMs = to.getTime() - from.getTime();
  const { labels, tooltipLabels, datasets } = useMemo(
    () =>
      buildTimeSeriesFromSummary(
        skillTimeSeries,
        (pt) => pt.targetLabel,
        timeRangeMs,
      ),
    [skillTimeSeries, timeRangeMs],
  );
  return (
    <ChartCard
      title="Skill Usage"
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={labels.length > 0}
    >
      <MultiLineChart
        labels={labels}
        tooltipLabels={tooltipLabels}
        datasets={datasets}
        height={
          expanded ? LINE_CHART_HEIGHT.expanded : LINE_CHART_HEIGHT.collapsed
        }
      />
    </ChartCard>
  );
}

function UsersPerSkillChart({
  title,
  skillBreakdown,
  expandedChart,
  onExpand,
}: {
  title: string;
  skillBreakdown: ToolUsageUsersByTargetRow[];
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const chartId = "users-per-skill";
  const expanded = expandedChart === chartId;
  const { labels, datasets } = useMemo(() => {
    const skillMap = new Map<string, Map<string, number>>();
    const userSet = new Set<string>();
    for (const row of skillBreakdown) {
      const user = row.userLabel || "unknown";
      userSet.add(user);
      const inner = skillMap.get(row.targetLabel) ?? new Map<string, number>();
      inner.set(user, (inner.get(user) ?? 0) + row.eventCount);
      skillMap.set(row.targetLabel, inner);
    }

    const sortedSkills = Array.from(skillMap.entries())
      .map(([skill, userCounts]) => ({
        skill,
        total: Array.from(userCounts.values()).reduce((a, b) => a + b, 0),
        userCounts,
      }))
      .sort((a, b) => b.total - a.total);

    const userTotals = new Map<string, number>();
    for (const user of userSet) {
      userTotals.set(
        user,
        sortedSkills.reduce((s, sk) => s + (sk.userCounts.get(user) ?? 0), 0),
      );
    }
    const sortedUsers = Array.from(userSet).sort(
      (a, b) => (userTotals.get(b) ?? 0) - (userTotals.get(a) ?? 0),
    );

    const chartLabels = sortedSkills.map((s) => s.skill);
    const chartDatasets = sortedUsers.map((user, i) => ({
      label: user,
      barThickness: BAR_THICKNESS.collapsed,
      data: sortedSkills.map((s) => s.userCounts.get(user) ?? 0),
      backgroundColor: USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length]!,
      hoverBackgroundColor:
        USER_SOURCE_COLORS[i % USER_SOURCE_COLORS.length]! + "cc",
    }));

    return { labels: chartLabels, datasets: chartDatasets };
  }, [skillBreakdown]);

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={labels.length > 0}
    >
      {labels.length === 0 ? (
        <ChartNoData />
      ) : (
        <StackedBarChart
          labels={labels}
          datasets={datasets}
          expanded={expanded}
          maxRows={COLLAPSED_BAR_CHART_MAX_ROWS}
          onShowAll={() => onExpand(chartId)}
        />
      )}
    </ChartCard>
  );
}

function ErrorsOverTimeChart({
  timeSeries,
  from,
  to,
  serverNameMappings,
  expandedChart,
  onExpand,
}: {
  timeSeries: ToolUsageTargetTimeSeriesPoint[];
  from: Date;
  to: Date;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
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
        const displayName = displayTargetLabel(
          pt.targetLabel,
          pt.targetType,
          serverNameMappings,
        );
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
    }, [timeSeries, timeRangeMs, serverNameMappings]);

  const chartId = "errors-over-time";
  const expanded = expandedChart === chartId;

  return (
    <ChartCard
      title="Errors Over Time"
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasErrors}
    >
      {!hasErrors ? (
        <ChartNoData message="No errors in this period" />
      ) : (
        <MultiLineChart
          labels={labels}
          tooltipLabels={tooltipLabels}
          datasets={datasets}
          height={
            expanded ? LINE_CHART_HEIGHT.expanded : LINE_CHART_HEIGHT.collapsed
          }
          tooltipAfterBody={(idx) => {
            const servers = perServerByIndex[idx];
            if (!servers || servers.length === 0) return [];
            return servers.map((s) => `${s.name}: ${s.count}`);
          }}
        />
      )}
    </ChartCard>
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
  expandedChart,
  onExpandedChartChange: setExpandedChart,
}: {
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  from: Date;
  to: Date;
  compact?: boolean;
  addFilter: (chip: FilterChip) => void;
  onHookTypesChange: (types: ObserveTypeFilterValue[]) => void;
  summaryData: GetToolUsageSummaryResult | undefined;
  summaryPending: boolean;
  summaryIsError: boolean;
  expandedChart: string | null;
  onExpandedChartChange: (id: string | null) => void;
}) {
  const targets = summaryData?.targets;
  const users = summaryData?.users ?? [];
  const timeSeries = summaryData?.targetTimeSeries ?? [];
  const userTimeSeries = summaryData?.userTimeSeries ?? [];
  const usersByTarget = summaryData?.usersByTarget ?? [];
  const targetToolBreakdown = summaryData?.targetToolBreakdown ?? [];
  const skillTimeSeries = timeSeries.filter(
    (point) => point.targetType === "skill",
  );
  const skillBreakdown = usersByTarget.filter(
    (row) => row.targetType === "skill",
  );

  const kpis = useMemo(() => {
    if (!summaryData) return null;
    const totalEvents = summaryData.totals.eventCount;
    const totalSuccesses = summaryData.totals.successCount;
    const totalFailures = summaryData.totals.failureCount;
    const completedEvents = totalSuccesses + totalFailures;
    const avgSuccessRate =
      completedEvents > 0 ? (totalSuccesses / completedEvents) * 100 : null;

    const activeUsers = summaryData.totals.uniqueUsers;
    const activeTargets = summaryData.totals.uniqueTargets;
    const uniqueTools = summaryData.totals.uniqueTools;

    return {
      avgSuccessRate,
      totalEvents,
      activeUsers,
      activeTargets,
      uniqueTools,
    };
  }, [summaryData]);

  const targetFiltersByLabel = useMemo(() => {
    const filters = new Map<string, string[]>();
    for (const target of targets ?? []) {
      const label = displayTargetLabel(
        target.targetLabel,
        target.targetType,
        serverNameMappings,
      );
      if (target.targetType === "hosted_mcp_server") {
        filters.set(label, [encodeHostedServerFilter(target.targetId)]);
      } else if (target.targetType === "shadow_mcp_server") {
        filters.set(label, [encodeShadowServerFilter(target.targetId)]);
      } else if (target.targetType === "local_tool") {
        filters.set(label, ["local_tool"]);
      } else if (target.targetType === "skill") {
        filters.set(label, ["skill"]);
      }
    }
    return filters;
  }, [serverNameMappings, targets]);

  type FilterAxisConfig = Partial<Record<"user" | "server", "dataset" | "row">>;

  const makeFilterHandler = useCallback(
    (config: FilterAxisConfig) => (datasetLabel: string, rowLabel: string) => {
      const localToolsDisplayName =
        serverNameMappings.rawToDisplay.get("") ?? "Local Tools";
      const apply = (value: string, filterType: "server" | "user") => {
        if (!value || value === "unknown") return;
        if (filterType === "server") {
          if (value === localToolsDisplayName) {
            onHookTypesChange(["local_tool"]);
            return;
          }
          const targetFilter = targetFiltersByLabel.get(value);
          if (targetFilter?.includes("local_tool")) {
            onHookTypesChange(["local_tool"]);
            return;
          }
          if (targetFilter?.includes("skill")) {
            onHookTypesChange(["skill"]);
            return;
          }
          const rawFilters = targetFilter ??
            serverNameMappings.displayToRaws.get(value) ?? [value];
          addFilter({
            display: value,
            filters: rawFilters,
            path: SERVER_FILTER_PATH,
          });
        } else {
          addFilter({
            display: value,
            filters: [value],
            path: USER_EMAIL_FILTER_PATH,
          });
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
      targetFiltersByLabel,
    ],
  );

  return (
    <div className="space-y-4">
      <div
        className={cn(
          "grid gap-3 transition-all duration-200 ease-in-out",
          compact
            ? "grid-cols-2 md:grid-cols-3"
            : "grid-cols-2 md:grid-cols-3 lg:grid-cols-5",
          expandedChart && "hidden",
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
              title="Active Targets"
              value={kpis?.activeTargets ?? 0}
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
        <ServerUsageTimeSeries
          timeSeries={timeSeries}
          from={from}
          to={to}
          serverNameMappings={serverNameMappings}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />

        <UsersPerServerChart
          title="Users per Target"
          breakdown={usersByTarget}
          serverNameMappings={serverNameMappings}
          handleFilter={makeFilterHandler({
            server: "row",
            user: "dataset",
          })}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />

        <UserUsageTimeSeries
          timeSeries={userTimeSeries}
          from={from}
          to={to}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />

        <UserEventCountsChart
          title="User Event Counts"
          users={users}
          handleFilter={makeFilterHandler({ user: "row" })}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />

        <SkillUsageTimeSeries
          skillTimeSeries={skillTimeSeries}
          from={from}
          to={to}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />

        <UsersPerSkillChart
          title="Users per Skill"
          skillBreakdown={skillBreakdown}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />

        <ErrorsOverTimeChart
          timeSeries={timeSeries}
          from={from}
          to={to}
          serverNameMappings={serverNameMappings}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />

        <ServerErrorRateChart
          title="Errors per Target and Tool"
          breakdown={targetToolBreakdown}
          serverNameMappings={serverNameMappings}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />
      </div>
    </div>
  );
}
