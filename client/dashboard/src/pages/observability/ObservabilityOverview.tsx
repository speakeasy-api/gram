import { Page } from "@/components/page-layout";
import {
  DateRangeSelect,
  DateRangePreset,
  getDateRange,
} from "@/pages/metrics/date-range-select";
import { Skeleton } from "@/components/ui/skeleton";
import { ServiceError } from "@gram/client/models/errors/serviceerror";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import { telemetryListFilterOptions } from "@gram/client/funcs/telemetryListFilterOptions";
import { useGramContext } from "@gram/client/react-query/_context";
import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import type { GetObservabilityOverviewResult } from "@gram/client/models/components";
import { FilterType } from "@gram/client/models/components/listfilteroptionspayload";
import { useState, useRef, useCallback, useMemo } from "react";
import { Icon, IconName } from "@speakeasy-api/moonshine";
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
import { Check, ChevronDown } from "lucide-react";
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  BarElement,
  Filler,
  Tooltip,
  Legend,
  type TooltipItem,
} from "chart.js";
import { Line, Bar } from "react-chartjs-2";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  Tooltip as UITooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { AreaChart, BarChart3, LineChart } from "lucide-react";

type ChartType = "area" | "bar" | "line";

function ChartTypeToggle({
  value,
  onChange,
}: {
  value: ChartType;
  onChange: (value: ChartType) => void;
}) {
  return (
    <TooltipProvider delayDuration={0}>
      <ToggleGroup
        type="single"
        value={value}
        onValueChange={(v) => v && onChange(v as ChartType)}
        variant="outline"
        size="sm"
      >
        <UITooltip>
          <TooltipTrigger asChild>
            <ToggleGroupItem
              value="area"
              aria-label="Area chart"
              className={`px-2 ${value === "area" ? "bg-muted" : ""}`}
            >
              <AreaChart
                className="size-4 text-muted-foreground"
                strokeWidth={1.75}
              />
            </ToggleGroupItem>
          </TooltipTrigger>
          <TooltipContent side="bottom" sideOffset={4}>
            Area
          </TooltipContent>
        </UITooltip>
        <UITooltip>
          <TooltipTrigger asChild>
            <ToggleGroupItem
              value="bar"
              aria-label="Bar chart"
              className={`px-2 ${value === "bar" ? "bg-muted" : ""}`}
            >
              <BarChart3
                className="size-4 text-muted-foreground"
                strokeWidth={1.75}
              />
            </ToggleGroupItem>
          </TooltipTrigger>
          <TooltipContent side="bottom" sideOffset={4}>
            Bar
          </TooltipContent>
        </UITooltip>
        <UITooltip>
          <TooltipTrigger asChild>
            <ToggleGroupItem
              value="line"
              aria-label="Line chart"
              className={`px-2 ${value === "line" ? "bg-muted" : ""}`}
            >
              <LineChart
                className="size-4 text-muted-foreground"
                strokeWidth={1.75}
              />
            </ToggleGroupItem>
          </TooltipTrigger>
          <TooltipContent side="bottom" sideOffset={4}>
            Line
          </TooltipContent>
        </UITooltip>
      </ToggleGroup>
    </TooltipProvider>
  );
}

// Register Chart.js components
ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  BarElement,
  Filler,
  Tooltip,
  Legend,
);

type FilterDimension = "all" | "api_key" | "user";

function FilterBar({
  dimension,
  onDimensionChange,
  selectedValue,
  onValueChange,
  options,
}: {
  dimension: FilterDimension;
  onDimensionChange: (dimension: FilterDimension) => void;
  selectedValue: string | null;
  onValueChange: (value: string | null) => void;
  options: Array<{ id: string; label: string; count: number }>;
}) {
  const [open, setOpen] = useState(false);

  const selectedOption = options.find((o) => o.id === selectedValue);
  const displayLabel = selectedOption
    ? selectedOption.label || selectedOption.id
    : `All ${dimension === "api_key" ? "API Keys" : "Users"}`;

  return (
    <div className="flex items-center gap-2">
      <span className="text-sm text-muted-foreground font-medium">
        Filter by
      </span>
      {/* Integrated segmented control with dropdown */}
      <div className="flex items-center h-9 bg-muted/50 rounded-md p-1 border border-border/50">
        {(["all", "api_key", "user"] as const).map((value) => {
          const isSelected = dimension === value;
          const label =
            value === "all" ? "All" : value === "api_key" ? "API Key" : "User";
          return (
            <button
              key={value}
              onClick={() => onDimensionChange(value)}
              className={`
                h-7 px-3 text-sm font-medium rounded-l transition-all duration-150
                ${value === "all" ? "rounded" : "rounded-l"}
                ${
                  isSelected
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }
              `}
            >
              {label}
            </button>
          );
        })}

        {/* Integrated searchable dropdown - appears as part of the control */}
        {dimension !== "all" && (
          <>
            <div className="w-px h-5 bg-border/50 mx-1" />
            <Popover open={open} onOpenChange={setOpen}>
              <PopoverTrigger asChild>
                <button className="h-7 min-w-[140px] flex items-center justify-between gap-2 text-sm px-2 hover:bg-muted/50 rounded transition-colors">
                  <span className="truncate max-w-[120px]">{displayLabel}</span>
                  <ChevronDown className="size-3.5 text-muted-foreground shrink-0" />
                </button>
              </PopoverTrigger>
              <PopoverContent className="w-[220px] p-0" align="end">
                <Command>
                  <CommandInput
                    placeholder={`Search ${dimension === "api_key" ? "API keys" : "users"}...`}
                    className="h-9"
                  />
                  <CommandList>
                    <CommandEmpty>No results found.</CommandEmpty>
                    <CommandGroup>
                      <CommandItem
                        value="__all__"
                        onSelect={() => {
                          onValueChange(null);
                          setOpen(false);
                        }}
                        className="cursor-pointer"
                      >
                        <Check
                          className={`mr-2 size-4 ${selectedValue === null ? "opacity-100" : "opacity-0"}`}
                        />
                        <span>
                          All {dimension === "api_key" ? "API Keys" : "Users"}
                        </span>
                      </CommandItem>
                      {options.map((option) => (
                        <CommandItem
                          key={option.id}
                          value={option.label || option.id}
                          onSelect={() => {
                            onValueChange(option.id);
                            setOpen(false);
                          }}
                          className="cursor-pointer"
                        >
                          <Check
                            className={`mr-2 size-4 ${selectedValue === option.id ? "opacity-100" : "opacity-0"}`}
                          />
                          <div className="flex items-center justify-between w-full gap-2">
                            <span className="truncate">
                              {option.label || option.id}
                            </span>
                            <span className="text-muted-foreground text-xs tabular-nums shrink-0">
                              {option.count.toLocaleString()}
                            </span>
                          </div>
                        </CommandItem>
                      ))}
                    </CommandGroup>
                  </CommandList>
                </Command>
              </PopoverContent>
            </Popover>
          </>
        )}
      </div>
    </div>
  );
}

/**
 * Apply a centered moving average to smooth data (like Datadog).
 * Window size auto-scales based on data length for consistent smoothing.
 */
function smoothData(data: number[], windowSize?: number): number[] {
  if (data.length < 3) return data;

  // Auto-scale window: ~8% of data points, min 5, max 21 for heavy smoothing
  const autoWindow = Math.max(5, Math.min(21, Math.floor(data.length * 0.08)));
  const window = windowSize ?? autoWindow;
  const halfWindow = Math.floor(window / 2);

  return data.map((_, i) => {
    const start = Math.max(0, i - halfWindow);
    const end = Math.min(data.length, i + halfWindow + 1);
    const slice = data.slice(start, end);
    return slice.reduce((a, b) => a + b, 0) / slice.length;
  });
}

export default function ObservabilityOverview() {
  const [dateRange, setDateRange] = useState<DateRangePreset>("30d");
  const [customRange, setCustomRange] = useState<{
    from: Date;
    to: Date;
  } | null>(null);
  const [filterDimension, setFilterDimension] =
    useState<FilterDimension>("all");
  const [selectedFilterValue, setSelectedFilterValue] = useState<string | null>(
    null,
  );

  // Use custom range if set, otherwise use preset
  const { from, to } = customRange ?? getDateRange(dateRange);
  const client = useGramContext();

  // Fetch filter options for the selected dimension (only when not "all")
  const { data: filterOptions } = useQuery({
    queryKey: [
      "observability",
      "filterOptions",
      filterDimension,
      customRange ?? dateRange,
    ],
    queryFn: () =>
      unwrapAsync(
        telemetryListFilterOptions(client, {
          listFilterOptionsPayload: {
            from,
            to,
            filterType:
              filterDimension === "api_key"
                ? FilterType.ApiKey
                : FilterType.User,
          },
        }),
      ),
    placeholderData: keepPreviousData,
    enabled: filterDimension !== "all",
  });

  // Build filter params based on selected dimension and value
  const filterParams = useMemo(() => {
    if (filterDimension === "all" || !selectedFilterValue) return {};
    if (filterDimension === "api_key") {
      return { apiKeyId: selectedFilterValue };
    } else {
      return { externalUserId: selectedFilterValue };
    }
  }, [filterDimension, selectedFilterValue]);

  const { data, isPending, isFetching, error } = useQuery({
    queryKey: [
      "observability",
      "overview",
      customRange ?? dateRange,
      filterParams,
    ],
    queryFn: () =>
      unwrapAsync(
        telemetryGetObservabilityOverview(client, {
          getObservabilityOverviewPayload: {
            from,
            to,
            includeTimeSeries: true,
            ...filterParams,
          },
        }),
      ),
    placeholderData: keepPreviousData,
  });

  // Reset selected filter value when dimension changes
  const handleFilterDimensionChange = useCallback(
    (dimension: FilterDimension) => {
      setFilterDimension(dimension);
      setSelectedFilterValue(null);
    },
    [],
  );

  const handleTimeRangeSelect = useCallback((newFrom: Date, newTo: Date) => {
    setCustomRange({ from: newFrom, to: newTo });
  }, []);

  const handleClearCustomRange = useCallback(() => {
    setCustomRange(null);
  }, []);

  const handlePresetChange = useCallback((preset: DateRangePreset) => {
    setDateRange(preset);
    setCustomRange(null);
  }, []);

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs fullWidth />
      </Page.Header>
      <Page.Body fullWidth className="space-y-6">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div className="flex flex-col gap-1">
            <div className="flex items-center gap-2">
              <h1 className="text-xl font-semibold">Observability Overview</h1>
              <span className="text-[10px] font-semibold uppercase tracking-wider px-1.5 py-0.5 rounded bg-amber-500/15 text-amber-500">
                Beta
              </span>
            </div>
            <p className="text-sm text-muted-foreground">
              Monitor chat sessions, tool performance, and system health
            </p>
          </div>
          <div className="flex items-center gap-3">
            <FilterBar
              dimension={filterDimension}
              onDimensionChange={handleFilterDimensionChange}
              selectedValue={selectedFilterValue}
              onValueChange={setSelectedFilterValue}
              options={filterOptions?.options ?? []}
            />
            <DateRangeSelect
              value={dateRange}
              onValueChange={handlePresetChange}
              customRange={customRange}
              onClearCustomRange={handleClearCustomRange}
            />
          </div>
        </div>

        <ObservabilityContent
          isPending={isPending}
          isFetching={isFetching}
          error={error}
          data={data}
          dateRange={dateRange}
          customRange={customRange}
          onTimeRangeSelect={handleTimeRangeSelect}
        />
      </Page.Body>
    </Page>
  );
}

function getComparisonLabel(
  dateRange: DateRangePreset,
  isCustomRange: boolean,
): string {
  if (isCustomRange) {
    return "vs previous period";
  }
  switch (dateRange) {
    case "24h":
      return "vs last 24 hours";
    case "7d":
      return "vs last 7 days";
    case "30d":
      return "vs last month";
    case "90d":
      return "vs last 3 months";
    default:
      return "vs previous period";
  }
}

function ObservabilityContent({
  isPending,
  isFetching,
  error,
  data,
  dateRange,
  customRange,
  onTimeRangeSelect,
}: {
  isPending: boolean;
  isFetching: boolean;
  error: Error | null;
  data: GetObservabilityOverviewResult | undefined;
  dateRange: DateRangePreset;
  customRange: { from: Date; to: Date } | null;
  onTimeRangeSelect: (from: Date, to: Date) => void;
}) {
  if (isPending) {
    return <LoadingSkeleton />;
  }

  if (error instanceof ServiceError && error.statusCode === 404) {
    return <DisabledState />;
  }

  if (error) {
    return <ErrorState error={error} />;
  }

  if (!data) {
    return null;
  }

  const {
    summary,
    comparison,
    timeSeries,
    topToolsByCount,
    topToolsByFailureRate,
  } = data;

  const comparisonLabel = getComparisonLabel(dateRange, customRange !== null);

  // Calculate error rate
  const errorRate =
    summary?.totalToolCalls && summary.totalToolCalls > 0
      ? ((summary.failedToolCalls ?? 0) / summary.totalToolCalls) * 100
      : 0;
  const previousErrorRate =
    comparison?.totalToolCalls && comparison.totalToolCalls > 0
      ? ((comparison.failedToolCalls ?? 0) / comparison.totalToolCalls) * 100
      : 0;

  // Show loading indicator when refetching (but keep showing old data)
  const isRefetching = isFetching && !isPending;

  // Calculate the actual time range for chart label formatting
  const { from, to } = customRange ?? getDateRange(dateRange);
  const timeRangeMs = to.getTime() - from.getTime();

  return (
    <div className="space-y-8">
      {/* ===== CHAT RESOLUTION SECTION ===== */}
      <section>
        <h2 className="text-lg font-semibold mb-4">Chat Resolution</h2>
        <div className="space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            <MetricCard
              title="Total Chats"
              value={summary?.totalChats ?? 0}
              previousValue={comparison?.totalChats ?? 0}
              icon="message-circle"
              thresholds={{ red: 10, amber: 50 }}
              comparisonLabel={comparisonLabel}
            />
            <MetricCard
              title="Resolution Rate"
              value={
                summary?.totalChats
                  ? ((summary.resolvedChats ?? 0) / summary.totalChats) * 100
                  : 0
              }
              previousValue={
                comparison?.totalChats
                  ? ((comparison.resolvedChats ?? 0) / comparison.totalChats) *
                    100
                  : 0
              }
              format="percent"
              icon="circle-check"
              thresholds={{ red: 30, amber: 60 }}
              comparisonLabel={comparisonLabel}
            />
            <MetricCard
              title="Avg Session Duration"
              value={(summary?.avgSessionDurationMs ?? 0) / 1000}
              previousValue={(comparison?.avgSessionDurationMs ?? 0) / 1000}
              format="seconds"
              icon="timer"
              invertDelta
              thresholds={{ red: 300, amber: 120, inverted: true }}
              comparisonLabel={comparisonLabel}
            />
            <MetricCard
              title="Avg Resolution Time"
              value={(summary?.avgResolutionTimeMs ?? 0) / 1000}
              previousValue={(comparison?.avgResolutionTimeMs ?? 0) / 1000}
              format="seconds"
              icon="clock"
              invertDelta
              thresholds={{ red: 180, amber: 60, inverted: true }}
              comparisonLabel={comparisonLabel}
            />
          </div>
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <ResolvedChatsChart
              data={timeSeries ?? []}
              timeRangeMs={timeRangeMs}
              title="Resolution Rate Over Time"
              onTimeRangeSelect={onTimeRangeSelect}
              isLoading={isRefetching}
            />
            <ResolutionStatusChart
              data={timeSeries ?? []}
              timeRangeMs={timeRangeMs}
              title="Chats by Resolution Status"
              onTimeRangeSelect={onTimeRangeSelect}
              isLoading={isRefetching}
            />
          </div>
          <SessionDurationChart
            data={timeSeries ?? []}
            timeRangeMs={timeRangeMs}
            title="Avg Session Duration Over Time"
            onTimeRangeSelect={onTimeRangeSelect}
            isLoading={isRefetching}
          />
        </div>
      </section>

      {/* ===== TOOL METRICS SECTION ===== */}
      <section>
        <h2 className="text-lg font-semibold mb-4">Tool Metrics</h2>
        <div className="space-y-4">
          <ToolCallsChart
            data={timeSeries ?? []}
            timeRangeMs={timeRangeMs}
            title="Tool Calls & Errors"
            onTimeRangeSelect={onTimeRangeSelect}
            isLoading={isRefetching}
          />
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <div className="rounded-lg border border-border bg-card p-6">
              <h3 className="text-sm font-semibold mb-4">Top Tools by Usage</h3>
              <ToolBarList
                tools={topToolsByCount ?? []}
                valueKey="callCount"
                valueLabel="calls"
              />
            </div>
            <div className="rounded-lg border border-border bg-card p-6">
              <h3 className="text-sm font-semibold mb-4">
                Tools by Failure Rate
              </h3>
              <ToolBarList
                tools={topToolsByFailureRate ?? []}
                valueKey="failureRate"
                valueLabel="%"
                isPercentage
              />
            </div>
          </div>
        </div>
      </section>

      {/* ===== SYSTEM METRICS SECTION ===== */}
      <section>
        <h2 className="text-lg font-semibold mb-4">System Metrics</h2>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <MetricCard
            title="Tool Calls"
            value={summary?.totalToolCalls ?? 0}
            previousValue={comparison?.totalToolCalls ?? 0}
            icon="wrench"
            thresholds={{ red: 10, amber: 50 }}
            comparisonLabel={comparisonLabel}
          />
          <MetricCard
            title="Avg Latency"
            value={summary?.avgLatencyMs ?? 0}
            previousValue={comparison?.avgLatencyMs ?? 0}
            format="ms"
            icon="clock"
            invertDelta
            thresholds={{ red: 500, amber: 250, inverted: true }}
            comparisonLabel={comparisonLabel}
          />
          <MetricCard
            title="Error Rate"
            value={errorRate}
            previousValue={previousErrorRate}
            format="percent"
            icon="triangle-alert"
            invertDelta
            thresholds={{ red: 10, amber: 5, inverted: true }}
            comparisonLabel={comparisonLabel}
          />
        </div>
      </section>
    </div>
  );
}

type ThresholdConfig = {
  red: number;
  amber: number;
  inverted?: boolean; // true if lower is better (like latency)
};

function getValueColor(value: number, thresholds?: ThresholdConfig): string {
  if (!thresholds) return "";

  if (thresholds.inverted) {
    // Lower is better (e.g., latency)
    if (value > thresholds.red) return "text-red-500";
    if (value > thresholds.amber) return "text-amber-500";
    return "text-emerald-600";
  } else {
    // Higher is better (e.g., chats, resolution rate)
    if (value < thresholds.red) return "text-red-500";
    if (value < thresholds.amber) return "text-amber-500";
    return "text-emerald-600";
  }
}

function MetricCard({
  title,
  value,
  previousValue,
  format = "number",
  icon,
  invertDelta = false,
  thresholds,
  comparisonLabel,
}: {
  title: string;
  value: number;
  previousValue: number;
  format?: "number" | "percent" | "ms" | "seconds";
  icon: IconName;
  invertDelta?: boolean;
  thresholds?: ThresholdConfig;
  comparisonLabel?: string;
}) {
  const formatValue = (v: number) => {
    switch (format) {
      case "percent":
        return `${v.toFixed(1)}%`;
      case "ms":
        return `${v.toFixed(0)}ms`;
      case "seconds":
        if (v >= 60) {
          const mins = Math.floor(v / 60);
          const secs = Math.round(v % 60);
          return secs > 0 ? `${mins}m ${secs}s` : `${mins}m`;
        }
        return `${v.toFixed(1)}s`;
      default:
        return v.toLocaleString();
    }
  };

  const rawDelta =
    previousValue > 0 ? ((value - previousValue) / previousValue) * 100 : 0;
  // Cap delta display at 999% to avoid absurd numbers
  const delta = Math.min(Math.abs(rawDelta), 999);
  const isPositive = rawDelta > 0;
  const isGood = invertDelta ? !isPositive : isPositive;

  const valueColor = getValueColor(value, thresholds);

  return (
    <div className="rounded-lg border border-border bg-card p-5">
      <div className="flex items-center justify-between mb-3">
        <span className="text-sm font-semibold">{title}</span>
        <div className="p-2 rounded-lg bg-muted/50">
          <Icon name={icon} className="size-4 text-muted-foreground" />
        </div>
      </div>
      <div className="flex items-end justify-between">
        <span className={`text-3xl font-semibold tracking-tight ${valueColor}`}>
          {formatValue(value)}
        </span>
        {previousValue > 0 && delta !== 0 && (
          <div className="flex flex-col items-end gap-0.5">
            <div
              className={`flex items-center gap-1 text-xs font-medium ${
                isGood ? "text-emerald-600" : "text-red-500"
              }`}
            >
              <Icon
                name={isPositive ? "trending-up" : "trending-down"}
                className="size-3"
              />
              <span>{delta.toFixed(1)}%</span>
            </div>
            {comparisonLabel && (
              <span className="text-[10px] text-muted-foreground">
                {comparisonLabel}
              </span>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function formatChartLabel(date: Date, timeRangeMs: number): string {
  const hours = timeRangeMs / (1000 * 60 * 60);
  const days = hours / 24;

  if (hours <= 24) {
    // ≤24 hours: Show time only "14:00"
    return date.toLocaleTimeString([], {
      hour: "2-digit",
      minute: "2-digit",
    });
  } else if (days <= 2) {
    // ≤2 days: Show date + time "Jan 5, 14:00"
    return date.toLocaleDateString([], {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  } else {
    // >2 days: Show date only "Jan 5"
    return date.toLocaleDateString([], { month: "short", day: "numeric" });
  }
}

// Chart selection wrapper for drag-to-zoom functionality
function ChartWithSelection({
  children,
  data,
  onTimeRangeSelect,
}: {
  children: React.ReactNode;
  data: Array<{ bucketTimeUnixNano?: string }>;
  onTimeRangeSelect?: (from: Date, to: Date) => void;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [selection, setSelection] = useState<{
    startX: number;
    currentX: number;
  } | null>(null);
  const [isDragging, setIsDragging] = useState(false);

  const handleMouseDown = useCallback(
    (e: React.MouseEvent) => {
      if (!onTimeRangeSelect || !containerRef.current) return;
      const rect = containerRef.current.getBoundingClientRect();
      const x = e.clientX - rect.left;
      setSelection({ startX: x, currentX: x });
      setIsDragging(true);
    },
    [onTimeRangeSelect],
  );

  const handleMouseMove = useCallback(
    (e: React.MouseEvent) => {
      if (!isDragging || !selection || !containerRef.current) return;
      const rect = containerRef.current.getBoundingClientRect();
      const x = Math.max(0, Math.min(e.clientX - rect.left, rect.width));
      setSelection((prev) => (prev ? { ...prev, currentX: x } : null));
    },
    [isDragging, selection],
  );

  const handleMouseUp = useCallback(() => {
    if (
      !isDragging ||
      !selection ||
      !containerRef.current ||
      !onTimeRangeSelect
    )
      return;

    const rect = containerRef.current.getBoundingClientRect();
    const startPercent =
      Math.min(selection.startX, selection.currentX) / rect.width;
    const endPercent =
      Math.max(selection.startX, selection.currentX) / rect.width;

    // Only trigger if selection is meaningful (> 5% of chart width)
    if (endPercent - startPercent > 0.05 && data.length > 0) {
      const startIndex = Math.floor(startPercent * data.length);
      const endIndex = Math.min(
        Math.ceil(endPercent * data.length),
        data.length - 1,
      );

      const startTimestamp =
        Number(data[startIndex]?.bucketTimeUnixNano) / 1_000_000;
      const endTimestamp =
        Number(data[endIndex]?.bucketTimeUnixNano) / 1_000_000;

      if (startTimestamp && endTimestamp) {
        onTimeRangeSelect(new Date(startTimestamp), new Date(endTimestamp));
      }
    }

    setSelection(null);
    setIsDragging(false);
  }, [isDragging, selection, data, onTimeRangeSelect]);

  const handleMouseLeave = useCallback(() => {
    if (isDragging) {
      setSelection(null);
      setIsDragging(false);
    }
  }, [isDragging]);

  const selectionLeft = selection
    ? Math.min(selection.startX, selection.currentX)
    : 0;
  const selectionWidth = selection
    ? Math.abs(selection.currentX - selection.startX)
    : 0;

  return (
    <div
      ref={containerRef}
      className="relative h-72 cursor-crosshair"
      onMouseDown={handleMouseDown}
      onMouseMove={handleMouseMove}
      onMouseUp={handleMouseUp}
      onMouseLeave={handleMouseLeave}
    >
      {children}
      {selection && selectionWidth > 5 && (
        <div
          className="absolute top-0 bottom-0 bg-blue-500/20 border-l border-r border-blue-500/50 pointer-events-none"
          style={{
            left: selectionLeft,
            width: selectionWidth,
          }}
        />
      )}
    </div>
  );
}

function ToolCallsChart({
  data,
  timeRangeMs,
  title,
  onTimeRangeSelect,
  isLoading,
}: {
  data: Array<{
    bucketTimeUnixNano?: string;
    totalToolCalls?: number;
    failedToolCalls?: number;
  }>;
  timeRangeMs: number;
  title: string;
  onTimeRangeSelect?: (from: Date, to: Date) => void;
  isLoading?: boolean;
}) {
  const [chartType, setChartType] = useState<ChartType>("area");

  const labels = data.map((d) => {
    const timestamp = Number(d.bucketTimeUnixNano) / 1_000_000;
    const date = new Date(timestamp);
    return formatChartLabel(date, timeRangeMs);
  });

  const rawToolCallsData = data.map(
    (d) => (d.totalToolCalls ?? 0) - (d.failedToolCalls ?? 0),
  );
  const rawErrorsData = data.map((d) => d.failedToolCalls ?? 0);

  const isArea = chartType === "area";
  const isBar = chartType === "bar";

  // Apply smoothing for line/area charts, use raw data for bar charts
  const toolCallsData = isBar ? rawToolCallsData : smoothData(rawToolCallsData);
  const errorsData = isBar ? rawErrorsData : smoothData(rawErrorsData);

  const toolCallsDataset = {
    label: " Tool Calls",
    data: toolCallsData,
    borderColor: "#3b82f6",
    backgroundColor: isBar ? "#3b82f6" : "rgba(59, 130, 246, 0.1)",
    pointBackgroundColor: "#3b82f6",
    fill: isArea,
    tension: 0.45,
    borderWidth: 1.5,
    pointRadius: 0,
    pointHoverRadius: 4,
    barPercentage: 1.0,
    categoryPercentage: 1.0,
  };

  const errorsDataset = {
    label: " Errors",
    data: errorsData,
    borderColor: "#ef4444",
    backgroundColor: isBar ? "#ef4444" : "rgba(239, 68, 68, 0.1)",
    pointBackgroundColor: "#ef4444",
    fill: isArea,
    tension: 0.45,
    borderWidth: 1.5,
    pointRadius: 0,
    pointHoverRadius: 4,
    barPercentage: 1.0,
    categoryPercentage: 1.0,
  };

  // With grouped:false, first dataset draws on top
  const chartData = {
    labels,
    datasets: isBar
      ? [errorsDataset, toolCallsDataset]
      : [toolCallsDataset, errorsDataset],
  };

  const options = {
    responsive: true,
    maintainAspectRatio: false,
    skipNull: true,
    interaction: {
      mode: "index" as const,
      intersect: false,
    },
    ...(isBar && {
      grouped: false,
    }),
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
          font: {
            size: 12,
          },
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
          label: (context: TooltipItem<"line"> | TooltipItem<"bar">) => {
            const label = context.dataset.label || "";
            const value = context.parsed.y ?? 0;
            return `${label}: ${Math.round(value).toLocaleString()}`;
          },
        },
      },
    },
    scales: {
      x: {
        grid: {
          display: false,
        },
        ticks: {
          maxTicksLimit: 8,
        },
      },
      y: {
        beginAtZero: true,
        grid: {
          color: "rgba(128, 128, 128, 0.2)",
        },
      },
    },
  };

  return (
    <div className="rounded-lg border border-border bg-card p-6">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-semibold">{title}</h3>
        <ChartTypeToggle value={chartType} onChange={setChartType} />
      </div>
      <div className="relative">
        {isLoading && (
          <div className="absolute inset-0 bg-background/60 z-10 flex items-center justify-center rounded">
            <div className="size-5 border-2 border-muted-foreground/50 border-t-transparent rounded-full animate-spin" />
          </div>
        )}
        <ChartWithSelection data={data} onTimeRangeSelect={onTimeRangeSelect}>
          {isBar ? (
            <Bar data={chartData} options={options} />
          ) : (
            <Line data={chartData} options={options} />
          )}
        </ChartWithSelection>
      </div>
    </div>
  );
}

function ResolvedChatsChart({
  data,
  timeRangeMs,
  title,
  onTimeRangeSelect,
  isLoading,
}: {
  data: Array<{
    bucketTimeUnixNano?: string;
    totalChats?: number;
    resolvedChats?: number;
    failedChats?: number;
  }>;
  timeRangeMs: number;
  title: string;
  onTimeRangeSelect?: (from: Date, to: Date) => void;
  isLoading?: boolean;
}) {
  const [chartType, setChartType] = useState<ChartType>("area");

  const labels = data.map((d) => {
    const timestamp = Number(d.bucketTimeUnixNano) / 1_000_000;
    const date = new Date(timestamp);
    return formatChartLabel(date, timeRangeMs);
  });

  // Calculate resolution rate %
  const rawResolvedPct = data.map((d) => {
    const total = d.totalChats ?? 0;
    if (total === 0) return null; // Return null for gaps (no data)
    return ((d.resolvedChats ?? 0) / total) * 100;
  });

  const isArea = chartType === "area";
  const isBar = chartType === "bar";

  // Apply smoothing for line/area charts, use raw data for bar charts
  // Filter out nulls for smoothing, then restore
  const nonNullData = rawResolvedPct.filter((v) => v !== null) as number[];
  const smoothedNonNull = smoothData(nonNullData);
  let smoothIdx = 0;
  const resolvedPctData = isBar
    ? rawResolvedPct
    : rawResolvedPct.map((v) =>
        v === null ? null : smoothedNonNull[smoothIdx++],
      );

  const chartData = {
    labels,
    datasets: [
      {
        label: " Resolution Rate",
        data: resolvedPctData,
        borderColor: "#10b981",
        backgroundColor: isBar ? "#10b981" : "rgba(16, 185, 129, 0.15)",
        pointBackgroundColor: "#10b981",
        fill: isArea,
        barPercentage: 1.0,
        categoryPercentage: 1.0,
        tension: 0.45,
        borderWidth: 2,
        pointRadius: 0,
        pointHoverRadius: 4,
        spanGaps: true,
      },
    ],
  };

  const options = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: {
      mode: "index" as const,
      intersect: false,
    },
    plugins: {
      legend: {
        display: false,
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
          label: (context: TooltipItem<"line"> | TooltipItem<"bar">) => {
            const value = context.parsed.y ?? 0;
            return ` Resolution Rate: ${value.toFixed(1)}%`;
          },
        },
      },
    },
    scales: {
      x: {
        grid: {
          display: false,
        },
        ticks: {
          maxTicksLimit: 8,
        },
      },
      y: {
        beginAtZero: true,
        max: 100,
        grid: {
          color: "rgba(128, 128, 128, 0.2)",
        },
        ticks: {
          callback: (value: number | string) => `${value}%`,
        },
      },
    },
  };

  return (
    <div className="rounded-lg border border-border bg-card p-6">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-semibold">{title}</h3>
        <ChartTypeToggle value={chartType} onChange={setChartType} />
      </div>
      <div className="relative">
        {isLoading && (
          <div className="absolute inset-0 bg-background/60 z-10 flex items-center justify-center rounded">
            <div className="size-5 border-2 border-muted-foreground/50 border-t-transparent rounded-full animate-spin" />
          </div>
        )}
        <ChartWithSelection data={data} onTimeRangeSelect={onTimeRangeSelect}>
          {isBar ? (
            <Bar data={chartData} options={options} />
          ) : (
            <Line data={chartData} options={options} />
          )}
        </ChartWithSelection>
      </div>
      <div className="flex items-center justify-between mt-3">
        <p className="text-xs text-muted-foreground">
          Percentage of chats successfully resolved per interval
        </p>
        <a
          href="#"
          className="text-xs text-primary hover:underline"
          onClick={(e) => e.preventDefault()}
        >
          View individual sessions →
        </a>
      </div>
    </div>
  );
}

function ResolutionStatusChart({
  data,
  timeRangeMs,
  title,
  onTimeRangeSelect,
  isLoading,
}: {
  data: Array<{
    bucketTimeUnixNano?: string;
    resolvedChats?: number;
    failedChats?: number;
    partialChats?: number;
    abandonedChats?: number;
  }>;
  timeRangeMs: number;
  title: string;
  onTimeRangeSelect?: (from: Date, to: Date) => void;
  isLoading?: boolean;
}) {
  const [chartType, setChartType] = useState<ChartType>("area");

  const labels = data.map((d) => {
    const timestamp = Number(d.bucketTimeUnixNano) / 1_000_000;
    const date = new Date(timestamp);
    return formatChartLabel(date, timeRangeMs);
  });

  const rawSuccessData = data.map((d) => d.resolvedChats ?? 0);
  const rawFailedData = data.map((d) => d.failedChats ?? 0);
  const rawPartialData = data.map((d) => d.partialChats ?? 0);
  const rawAbandonedData = data.map((d) => d.abandonedChats ?? 0);

  const isArea = chartType === "area";
  const isBar = chartType === "bar";

  // Apply smoothing for line/area charts, use raw data for bar charts
  const successData = isBar ? rawSuccessData : smoothData(rawSuccessData);
  const failedData = isBar ? rawFailedData : smoothData(rawFailedData);
  const partialData = isBar ? rawPartialData : smoothData(rawPartialData);
  const abandonedData = isBar ? rawAbandonedData : smoothData(rawAbandonedData);

  const chartData = {
    labels,
    datasets: [
      {
        label: " Success",
        data: successData,
        borderColor: "#10b981",
        backgroundColor: isBar ? "#10b981" : "rgba(16, 185, 129, 0.1)",
        pointBackgroundColor: "#10b981",
        fill: isArea,
        tension: 0.45,
        borderWidth: 1.5,
        pointRadius: 0,
        pointHoverRadius: 4,
        barPercentage: 0.8,
        categoryPercentage: 0.9,
      },
      {
        label: " Failed",
        data: failedData,
        borderColor: "#ef4444",
        backgroundColor: isBar ? "#ef4444" : "rgba(239, 68, 68, 0.1)",
        pointBackgroundColor: "#ef4444",
        fill: isArea,
        tension: 0.45,
        borderWidth: 1.5,
        pointRadius: 0,
        pointHoverRadius: 4,
        barPercentage: 0.8,
        categoryPercentage: 0.9,
      },
      {
        label: " Partial",
        data: partialData,
        borderColor: "#f59e0b",
        backgroundColor: isBar ? "#f59e0b" : "rgba(245, 158, 11, 0.1)",
        pointBackgroundColor: "#f59e0b",
        fill: isArea,
        tension: 0.45,
        borderWidth: 1.5,
        pointRadius: 0,
        pointHoverRadius: 4,
        barPercentage: 0.8,
        categoryPercentage: 0.9,
      },
      {
        label: " Abandoned",
        data: abandonedData,
        borderColor: "#6b7280",
        backgroundColor: isBar ? "#6b7280" : "rgba(107, 114, 128, 0.1)",
        pointBackgroundColor: "#6b7280",
        fill: isArea,
        tension: 0.45,
        borderWidth: 1.5,
        pointRadius: 0,
        pointHoverRadius: 4,
        barPercentage: 0.8,
        categoryPercentage: 0.9,
      },
    ],
  };

  const options = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: {
      mode: "index" as const,
      intersect: false,
    },
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
          font: {
            size: 12,
          },
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
          label: (context: TooltipItem<"line"> | TooltipItem<"bar">) => {
            const label = context.dataset.label || "";
            const value = context.parsed.y ?? 0;
            return `${label}: ${Math.round(value).toLocaleString()} chats`;
          },
        },
      },
    },
    scales: {
      x: {
        grid: {
          display: false,
        },
        ticks: {
          maxTicksLimit: 8,
        },
      },
      y: {
        beginAtZero: true,
        grid: {
          color: "rgba(128, 128, 128, 0.2)",
        },
        ticks: {
          callback: (value: number | string) => {
            const num = typeof value === "string" ? parseFloat(value) : value;
            return num.toLocaleString();
          },
        },
      },
    },
  };

  return (
    <div className="rounded-lg border border-border bg-card p-6">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-semibold">{title}</h3>
        <ChartTypeToggle value={chartType} onChange={setChartType} />
      </div>
      <div className="relative">
        {isLoading && (
          <div className="absolute inset-0 bg-background/60 z-10 flex items-center justify-center rounded">
            <div className="size-5 border-2 border-muted-foreground/50 border-t-transparent rounded-full animate-spin" />
          </div>
        )}
        <ChartWithSelection data={data} onTimeRangeSelect={onTimeRangeSelect}>
          {isBar ? (
            <Bar data={chartData} options={options} />
          ) : (
            <Line data={chartData} options={options} />
          )}
        </ChartWithSelection>
      </div>
      <p className="text-xs text-muted-foreground mt-3">
        Chat counts by resolution status over time
      </p>
    </div>
  );
}

function SessionDurationChart({
  data,
  timeRangeMs,
  title,
  onTimeRangeSelect,
  isLoading,
}: {
  data: Array<{
    bucketTimeUnixNano?: string;
    avgSessionDurationMs?: number;
  }>;
  timeRangeMs: number;
  title: string;
  onTimeRangeSelect?: (from: Date, to: Date) => void;
  isLoading?: boolean;
}) {
  const [chartType, setChartType] = useState<ChartType>("area");

  const labels = data.map((d) => {
    const timestamp = Number(d.bucketTimeUnixNano) / 1_000_000;
    const date = new Date(timestamp);
    return formatChartLabel(date, timeRangeMs);
  });

  // Convert ms to seconds for display
  const rawData = data.map((d) => (d.avgSessionDurationMs ?? 0) / 1000);

  const isArea = chartType === "area";
  const isBar = chartType === "bar";

  // Apply smoothing for line/area charts, use raw data for bar charts
  const durationData = isBar ? rawData : smoothData(rawData);

  const chartData = {
    labels,
    datasets: [
      {
        label: " Avg Duration",
        data: durationData,
        borderColor: "#8b5cf6",
        backgroundColor: isBar ? "#8b5cf6" : "rgba(139, 92, 246, 0.1)",
        pointBackgroundColor: "#8b5cf6",
        fill: isArea,
        barPercentage: 1.0,
        categoryPercentage: 1.0,
        tension: 0.45,
        borderWidth: 1.5,
        pointRadius: 0,
        pointHoverRadius: 4,
      },
    ],
  };

  const formatDuration = (seconds: number) => {
    if (seconds >= 60) {
      const mins = Math.floor(seconds / 60);
      const secs = Math.round(seconds % 60);
      return secs > 0 ? `${mins}m ${secs}s` : `${mins}m`;
    }
    return `${seconds.toFixed(1)}s`;
  };

  const options = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: {
      mode: "index" as const,
      intersect: false,
    },
    plugins: {
      legend: {
        display: false,
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
          label: (context: TooltipItem<"line"> | TooltipItem<"bar">) => {
            const value = context.parsed.y ?? 0;
            return ` Avg Duration: ${formatDuration(value)}`;
          },
        },
      },
    },
    scales: {
      x: {
        grid: {
          display: false,
        },
        ticks: {
          maxTicksLimit: 8,
        },
      },
      y: {
        beginAtZero: true,
        grid: {
          color: "rgba(128, 128, 128, 0.2)",
        },
        ticks: {
          callback: (value: number | string) => {
            const num = typeof value === "string" ? parseFloat(value) : value;
            return formatDuration(num);
          },
        },
      },
    },
  };

  return (
    <div className="rounded-lg border border-border bg-card p-6">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-semibold">{title}</h3>
        <ChartTypeToggle value={chartType} onChange={setChartType} />
      </div>
      <div className="relative">
        {isLoading && (
          <div className="absolute inset-0 bg-background/60 z-10 flex items-center justify-center rounded">
            <div className="size-5 border-2 border-muted-foreground/50 border-t-transparent rounded-full animate-spin" />
          </div>
        )}
        <ChartWithSelection data={data} onTimeRangeSelect={onTimeRangeSelect}>
          {isBar ? (
            <Bar data={chartData} options={options} />
          ) : (
            <Line data={chartData} options={options} />
          )}
        </ChartWithSelection>
      </div>
      <p className="text-xs text-muted-foreground mt-3">
        Values are rolled up and averaged across the time window interval
      </p>
    </div>
  );
}

// Brand-inspired muted palette (from moonshine gradient colors)
const barColors = [
  "bg-[hsl(214,69%,50%)]", // Java blue
  "bg-[hsl(4,67%,52%)]", // Swift red
  "bg-[hsl(108,35%,45%)]", // Terraform green
  "bg-[hsl(216,70%,60%)]", // Python blue
  "bg-[hsl(23,80%,55%)]", // Ruby orange
  "bg-[hsl(334,50%,45%)]", // PHP magenta
  "bg-[hsl(68,45%,50%)]", // Unity lime
  "bg-[hsl(154,50%,40%)]", // C teal
  "bg-[hsl(220,60%,45%)]", // Go blue
  "bg-[hsl(280,40%,50%)]", // Purple accent
];

function ToolBarList({
  tools,
  valueKey,
  valueLabel,
  isPercentage = false,
}: {
  tools: Array<{
    gramUrn?: string;
    callCount?: number;
    failureRate?: number;
  }>;
  valueKey: "callCount" | "failureRate";
  valueLabel: string;
  isPercentage?: boolean;
}) {
  const barListData = tools.slice(0, 10).map((tool) => {
    const rawValue = tool[valueKey] ?? 0;
    const value = isPercentage ? rawValue * 100 : rawValue;
    return {
      name: tool.gramUrn?.replace("tools:", "") ?? "Unknown",
      value,
    };
  });

  if (barListData.length === 0) {
    return (
      <div className="text-center text-muted-foreground py-8">
        No tool data available
      </div>
    );
  }

  const maxValue = Math.max(...barListData.map((d) => d.value));

  return (
    <div className="space-y-2">
      {barListData.map((item, index) => {
        const widthPercent = maxValue > 0 ? (item.value / maxValue) * 100 : 0;
        const displayValue = isPercentage
          ? `${item.value.toFixed(1)}${valueLabel}`
          : item.value.toLocaleString();

        return (
          <div key={item.name} className="flex items-center gap-2">
            <span className="text-sm font-medium text-right shrink-0 min-w-[3rem]">
              {displayValue}
            </span>
            <div className="flex-1 relative h-7">
              {/* Background text (for overflow outside bar) */}
              <span className="absolute inset-y-0 left-2 flex items-center text-sm font-medium text-foreground truncate pr-2 z-0">
                {item.name}
              </span>
              {/* Colored bar */}
              <div
                className={`absolute inset-y-0 left-0 rounded ${barColors[index % barColors.length]}`}
                style={{ width: `${Math.max(widthPercent, 5)}%` }}
              />
              {/* White text clipped to bar */}
              <div
                className="absolute inset-y-0 left-0 overflow-hidden z-10"
                style={{ width: `${Math.max(widthPercent, 5)}%` }}
              >
                <span className="absolute inset-y-0 left-2 flex items-center text-sm font-medium text-white truncate pr-2 whitespace-nowrap">
                  {item.name}
                </span>
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

function LoadingSkeleton() {
  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        {[1, 2, 3, 4].map((i) => (
          <div key={i} className="rounded-lg border border-border bg-card p-5">
            <Skeleton className="h-4 w-24 mb-3" />
            <Skeleton className="h-9 w-32" />
          </div>
        ))}
      </div>
      <div className="rounded-lg border border-border bg-card p-6">
        <Skeleton className="h-72 w-full" />
      </div>
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="rounded-lg border border-border bg-card p-6">
          <Skeleton className="h-64 w-full" />
        </div>
        <div className="rounded-lg border border-border bg-card p-6">
          <Skeleton className="h-64 w-full" />
        </div>
      </div>
    </div>
  );
}

function DisabledState() {
  return (
    <div className="flex flex-col items-center justify-center py-16">
      <Icon
        name="chart-no-axes-combined"
        className="size-12 text-muted-foreground mb-4"
      />
      <h3 className="text-lg font-medium mb-2">Observability Not Enabled</h3>
      <p className="text-muted-foreground text-center max-w-md">
        Enable logs for your organization to access observability metrics and
        insights.
      </p>
    </div>
  );
}

function ErrorState({ error }: { error: Error }) {
  return (
    <div className="flex flex-col items-center justify-center py-16">
      <Icon name="triangle-alert" className="size-12 text-destructive mb-4" />
      <h3 className="text-lg font-medium mb-2">Error Loading Data</h3>
      <p className="text-muted-foreground text-center max-w-md">
        {error.message}
      </p>
    </div>
  );
}
