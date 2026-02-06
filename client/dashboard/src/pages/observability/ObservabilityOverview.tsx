import { Page } from "@/components/page-layout";
import {
  DateRangeSelect,
  DateRangePreset,
  getDateRange,
} from "@/pages/metrics/date-range-select";
import { Skeleton } from "@/components/ui/skeleton";
import { ServiceError } from "@gram/client/models/errors/serviceerror";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import { useGramContext } from "@gram/client/react-query/_context";
import { useQuery } from "@tanstack/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import type { GetObservabilityOverviewResult } from "@gram/client/models/components";
import { useState } from "react";
import { Icon, IconName } from "@speakeasy-api/moonshine";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

type ChartType = "area" | "bar" | "line";

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

export default function ObservabilityOverview() {
  const [dateRange, setDateRange] = useState<DateRangePreset>("7d");

  const { from, to } = getDateRange(dateRange);
  const client = useGramContext();

  const { data, isPending, error } = useQuery({
    queryKey: ["observability", "overview", dateRange],
    queryFn: () =>
      unwrapAsync(
        telemetryGetObservabilityOverview(client, {
          getObservabilityOverviewPayload: {
            from,
            to,
            includeTimeSeries: true,
          },
        }),
      ),
  });

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
          <DateRangeSelect value={dateRange} onValueChange={setDateRange} />
        </div>

        <ObservabilityContent
          isPending={isPending}
          error={error}
          data={data}
          dateRange={dateRange}
        />
      </Page.Body>
    </Page>
  );
}

function getComparisonLabel(dateRange: DateRangePreset): string {
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
  error,
  data,
  dateRange,
}: {
  isPending: boolean;
  error: Error | null;
  data: GetObservabilityOverviewResult | undefined;
  dateRange: DateRangePreset;
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

  const comparisonLabel = getComparisonLabel(dateRange);

  // Calculate error rate
  const errorRate =
    summary?.totalToolCalls && summary.totalToolCalls > 0
      ? ((summary.failedToolCalls ?? 0) / summary.totalToolCalls) * 100
      : 0;
  const previousErrorRate =
    comparison?.totalToolCalls && comparison.totalToolCalls > 0
      ? ((comparison.failedToolCalls ?? 0) / comparison.totalToolCalls) * 100
      : 0;

  return (
    <div className="space-y-8">
      {/* ===== CHAT RESOLUTION SECTION ===== */}
      <section>
        <h2 className="text-lg font-semibold mb-4">Chat Resolution</h2>
        <div className="space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
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
          </div>
          <ResolutionRateChart
            data={timeSeries ?? []}
            dateRange={dateRange}
            title="Resolution rate over time"
          />
        </div>
      </section>

      {/* ===== TOOL METRICS SECTION ===== */}
      <section>
        <h2 className="text-lg font-semibold mb-4">Tool Metrics</h2>
        <div className="space-y-4">
          <ToolCallsChart
            data={timeSeries ?? []}
            dateRange={dateRange}
            title="Tool calls & errors"
          />
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <div className="rounded-lg border border-border bg-card p-6">
              <h3 className="text-sm font-medium text-muted-foreground mb-4">
                Top tools by usage
              </h3>
              <ToolBarList
                tools={topToolsByCount ?? []}
                valueKey="callCount"
                valueLabel="calls"
              />
            </div>
            <div className="rounded-lg border border-border bg-card p-6">
              <h3 className="text-sm font-medium text-muted-foreground mb-4">
                Tools by failure rate
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
  format?: "number" | "percent" | "ms";
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
        <span className="text-sm font-medium text-muted-foreground">
          {title}
        </span>
        <div className="p-2 rounded-lg bg-muted/50">
          <Icon name={icon} className="size-4 text-muted-foreground" />
        </div>
      </div>
      <div className="flex items-end justify-between">
        <span
          className={`text-3xl font-semibold tracking-tight ${valueColor}`}
        >
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

function formatChartLabel(date: Date, dateRange: DateRangePreset): string {
  switch (dateRange) {
    case "24h":
      // Time only: "14:00"
      return date.toLocaleTimeString([], {
        hour: "2-digit",
        minute: "2-digit",
      });
    case "7d":
      // Date only: "Jan 5"
      return date.toLocaleDateString([], { month: "short", day: "numeric" });
    case "30d":
    case "90d":
    default:
      // Date only: "Jan 5"
      return date.toLocaleDateString([], { month: "short", day: "numeric" });
  }
}

function ToolCallsChart({
  data,
  dateRange,
  title,
}: {
  data: Array<{
    bucketTimeUnixNano?: string;
    totalToolCalls?: number;
    failedToolCalls?: number;
  }>;
  dateRange: DateRangePreset;
  title: string;
}) {
  const [chartType, setChartType] = useState<ChartType>("area");

  const labels = data.map((d) => {
    const timestamp = Number(d.bucketTimeUnixNano) / 1_000_000;
    const date = new Date(timestamp);
    return formatChartLabel(date, dateRange);
  });

  const toolCallsData = data.map(
    (d) => (d.totalToolCalls ?? 0) - (d.failedToolCalls ?? 0),
  );
  const errorsData = data.map((d) => d.failedToolCalls ?? 0);

  const isArea = chartType === "area";
  const isBar = chartType === "bar";

  const toolCallsDataset = {
    label: " Tool Calls",
    data: toolCallsData,
    borderColor: "#3b82f6",
    backgroundColor: isBar ? "#3b82f6" : "rgba(59, 130, 246, 0.1)",
    pointBackgroundColor: "#3b82f6",
    fill: isArea,
    tension: 0.3,
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
    tension: 0.3,
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
          usePointStyle: true,
          pointStyle: "circle",
          boxWidth: 8,
          padding: 15,
          color: "#374151",
          font: {
            size: 12,
          },
        },
      },
      tooltip: {
        backgroundColor: "white",
        titleColor: "#111",
        bodyColor: "#666",
        borderColor: "#e5e7eb",
        borderWidth: 1,
        padding: 12,
        boxPadding: 4,
        usePointStyle: true,
        callbacks: {
          label: (context: TooltipItem<"line"> | TooltipItem<"bar">) => {
            const label = context.dataset.label || "";
            const value = context.parsed.y ?? 0;
            return `${label}: ${value.toLocaleString()}`;
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
          color: "#f3f4f6",
        },
      },
    },
  };

  return (
    <div className="rounded-lg border border-border bg-card p-6">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-medium text-muted-foreground">{title}</h3>
        <Select
          value={chartType}
          onValueChange={(v) => setChartType(v as ChartType)}
        >
          <SelectTrigger className="w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="area">Area</SelectItem>
            <SelectItem value="bar">Bar</SelectItem>
            <SelectItem value="line">Line</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="h-72">
        {isBar ? (
          <Bar data={chartData} options={options} />
        ) : (
          <Line data={chartData} options={options} />
        )}
      </div>
    </div>
  );
}

function ResolutionRateChart({
  data,
  dateRange,
  title,
}: {
  data: Array<{
    bucketTimeUnixNano?: string;
    totalChats?: number;
    resolvedChats?: number;
  }>;
  dateRange: DateRangePreset;
  title: string;
}) {
  const [chartType, setChartType] = useState<ChartType>("area");

  const labels = data.map((d) => {
    const timestamp = Number(d.bucketTimeUnixNano) / 1_000_000;
    const date = new Date(timestamp);
    return formatChartLabel(date, dateRange);
  });

  const resolutionRateData = data.map((d) => {
    const total = d.totalChats ?? 0;
    const resolved = d.resolvedChats ?? 0;
    return total > 0 ? (resolved / total) * 100 : 0;
  });

  const isArea = chartType === "area";
  const isBar = chartType === "bar";

  const chartData = {
    labels,
    datasets: [
      {
        label: " Resolution Rate",
        data: resolutionRateData,
        borderColor: "#10b981",
        backgroundColor: isBar ? "#10b981" : "rgba(16, 185, 129, 0.1)",
        pointBackgroundColor: "#10b981",
        fill: isArea,
        barPercentage: 1.0,
        categoryPercentage: 1.0,
        tension: 0.3,
        borderWidth: 1.5,
        pointRadius: 0,
        pointHoverRadius: 4,
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
        backgroundColor: "white",
        titleColor: "#111",
        bodyColor: "#666",
        borderColor: "#e5e7eb",
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
          color: "#f3f4f6",
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
        <h3 className="text-sm font-medium text-muted-foreground">{title}</h3>
        <Select
          value={chartType}
          onValueChange={(v) => setChartType(v as ChartType)}
        >
          <SelectTrigger className="w-28">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="area">Area</SelectItem>
            <SelectItem value="bar">Bar</SelectItem>
            <SelectItem value="line">Line</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="h-72">
        {isBar ? (
          <Bar data={chartData} options={options} />
        ) : (
          <Line data={chartData} options={options} />
        )}
      </div>
    </div>
  );
}

// Vibrant color palette for bars (similar to Datadog)
const barColors = [
  "bg-cyan-400",
  "bg-fuchsia-400",
  "bg-amber-400",
  "bg-emerald-400",
  "bg-rose-400",
  "bg-violet-400",
  "bg-orange-400",
  "bg-sky-400",
  "bg-lime-400",
  "bg-pink-400",
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
          <div key={item.name} className="flex items-center gap-3">
            <span className="text-sm font-medium tabular-nums w-14 text-right shrink-0">
              {displayValue}
            </span>
            <div className="flex-1 relative h-7">
              <div
                className={`absolute inset-y-0 left-0 rounded ${barColors[index % barColors.length]}`}
                style={{ width: `${Math.max(widthPercent, 5)}%` }}
              />
              <span className="absolute inset-y-0 left-2 flex items-center text-sm font-medium text-gray-900 truncate pr-2">
                {item.name}
              </span>
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
