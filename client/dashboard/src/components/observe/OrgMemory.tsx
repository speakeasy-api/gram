import { ChartCard } from "@/components/chart/ChartCard";
import { formatChartLabel, smoothData } from "@/components/chart/chartUtils";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { useTelemetry } from "@/contexts/Telemetry";
import { formatCompact } from "@/lib/format";
import { formatUsageCost } from "@/pages/chatLogs/claudeUsage";
import { useRoutes } from "@/routes";
import type { WorkUnitsTrendBucket } from "@gram/client/models/components/workunitstrendbucket.js";
import { useWorkUnitsTrend } from "@gram/client/react-query/workUnitsTrend.js";
import {
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
  type ChartDataset,
  type ChartOptions,
} from "chart.js";
import { useMemo, useState } from "react";
import { Chart } from "react-chartjs-2";
import { Navigate } from "react-router";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  LineElement,
  PointElement,
  Filler,
  Tooltip,
  Legend,
);

const TREND_WINDOW_DAYS = 30;
const TREND_WINDOW_MS = TREND_WINDOW_DAYS * 24 * 60 * 60 * 1000;

function WorkDoneChart({
  buckets,
  loading,
  error,
  expandedChart,
  onExpand,
}: {
  buckets: WorkUnitsTrendBucket[];
  loading: boolean;
  error: boolean;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}): JSX.Element {
  const hasData = buckets.some((b) => b.scoredSessions > 0);

  const chartData = useMemo<{
    labels: string[];
    datasets: Array<
      ChartDataset<"bar", number[]> | ChartDataset<"line", number[]>
    >;
  }>(() => {
    const labels = buckets.map((b) =>
      formatChartLabel(b.timestamp, TREND_WINDOW_MS),
    );
    const bars: ChartDataset<"bar", number[]> = {
      label: "Work delivered",
      data: buckets.map((b) => b.workUnits),
      backgroundColor: "rgba(96, 165, 250, 0.35)",
      order: 2,
    };
    const trend: ChartDataset<"line", number[]> = {
      label: "Trend",
      data: smoothData(buckets.map((b) => b.workUnits)),
      type: "line",
      borderColor: "#3b82f6",
      backgroundColor: "transparent",
      pointRadius: 0,
      pointHoverRadius: 4,
      borderWidth: 2,
      tension: 0.4,
      fill: false,
      order: 1,
    };
    return { labels, datasets: [bars, trend] };
  }, [buckets]);

  const options = useMemo<ChartOptions<"bar">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: {
          position: "bottom",
          labels: {
            boxWidth: 12,
            usePointStyle: true,
            padding: 16,
            font: { size: 11 },
            filter: (legendItem) => legendItem.text !== "Trend",
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
            label: (item) =>
              ` ${item.dataset.label}: ${formatCompact(Number(item.parsed.y ?? 0))}`,
          },
        },
      },
      scales: {
        x: {
          grid: { display: true, color: "rgba(128, 128, 128, 0.08)" },
          ticks: { maxTicksLimit: 8 },
        },
        y: {
          beginAtZero: true,
          grid: { color: "rgba(128, 128, 128, 0.15)" },
          ticks: { callback: (value) => formatCompact(Number(value)) },
        },
      },
    }),
    [],
  );

  return (
    <ChartCard
      title="Work delivered"
      chartId="org-memory-work-done"
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasData}
      loading={loading}
      error={error}
    >
      {!hasData ? (
        <WidgetEmptyState
          message="No scored sessions in the selected time range"
          className="h-[260px]"
        />
      ) : (
        <div style={{ height: expandedChart ? 420 : 260 }}>
          <Chart<"bar" | "line", number[], string>
            type="bar"
            data={chartData}
            options={options}
          />
        </div>
      )}
    </ChartCard>
  );
}

function EfficiencyChart({
  buckets,
  loading,
  error,
  expandedChart,
  onExpand,
}: {
  buckets: WorkUnitsTrendBucket[];
  loading: boolean;
  error: boolean;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}): JSX.Element {
  const hasData = buckets.some(
    (b) => b.costPerUnit !== undefined || b.tokensPerUnit !== undefined,
  );

  const chartData = useMemo<{
    labels: string[];
    datasets: Array<ChartDataset<"line", Array<number | null>>>;
  }>(() => {
    const labels = buckets.map((b) =>
      formatChartLabel(b.timestamp, TREND_WINDOW_MS),
    );
    return {
      labels,
      datasets: [
        {
          label: "Cost efficiency",
          data: buckets.map((b) => b.costPerUnit ?? null),
          borderColor: "#34d399",
          backgroundColor: "transparent",
          pointRadius: 2,
          pointHoverRadius: 4,
          borderWidth: 2,
          tension: 0.3,
          spanGaps: true,
          yAxisID: "y",
        },
        {
          label: "Token efficiency",
          data: buckets.map((b) => b.tokensPerUnit ?? null),
          borderColor: "#a78bfa",
          backgroundColor: "transparent",
          pointRadius: 2,
          pointHoverRadius: 4,
          borderWidth: 2,
          tension: 0.3,
          spanGaps: true,
          yAxisID: "yTokens",
        },
      ],
    };
  }, [buckets]);

  const options = useMemo<ChartOptions<"line">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: {
          position: "bottom",
          labels: {
            boxWidth: 12,
            usePointStyle: true,
            padding: 16,
            font: { size: 11 },
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
            label: (item) => {
              const value = Number(item.parsed.y ?? 0);
              if (item.dataset.label === "Cost efficiency") {
                return ` Cost efficiency: ${formatUsageCost(value)}`;
              }
              return ` Token efficiency: ${formatCompact(value)}`;
            },
          },
        },
      },
      scales: {
        x: {
          grid: { display: true, color: "rgba(128, 128, 128, 0.08)" },
          ticks: { maxTicksLimit: 8 },
        },
        y: {
          beginAtZero: true,
          position: "left",
          grid: { color: "rgba(128, 128, 128, 0.15)" },
          ticks: { callback: (value) => formatUsageCost(Number(value)) },
        },
        yTokens: {
          beginAtZero: true,
          position: "right",
          grid: { display: false },
          ticks: { callback: (value) => formatCompact(Number(value)) },
        },
      },
    }),
    [],
  );

  return (
    <ChartCard
      title="Efficiency"
      chartId="org-memory-efficiency"
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasData}
      loading={loading}
      error={error}
    >
      {!hasData ? (
        <WidgetEmptyState
          message="No efficiency data in the selected time range"
          className="h-[260px]"
        />
      ) : (
        <div style={{ height: expandedChart ? 420 : 260 }}>
          <Chart<"line", Array<number | null>, string>
            type="line"
            data={chartData}
            options={options}
          />
        </div>
      )}
    </ChartCard>
  );
}

export default function OrgMemory(): JSX.Element {
  const telemetry = useTelemetry();
  const routes = useRoutes();
  const [expandedChart, setExpandedChart] = useState<string | null>(null);

  // Fixed 30-day window, computed once per mount so the query key is stable.
  const { from, to } = useMemo(() => {
    const now = new Date();
    return { from: new Date(now.getTime() - TREND_WINDOW_MS), to: now };
  }, []);

  const { data, isLoading, error } = useWorkUnitsTrend(
    { from, to },
    undefined,
    {
      throwOnError: false,
    },
  );

  // `=== false` so the page still renders while the flag is resolving.
  if (telemetry.isFeatureEnabled("org-memory") === false) {
    return <Navigate to={routes.home.href()} replace />;
  }

  const buckets = data?.buckets ?? [];
  const scoresAvailable = data?.scoresAvailable ?? false;

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 p-6">
      <div>
        <div className="flex items-center gap-2">
          <h1 className="text-xl font-semibold">Org Memory</h1>
          <ReleaseStageBadge stage="preview" />
        </div>
        <p className="text-muted-foreground mt-1 text-sm">
          How much work your agents deliver and what it costs, judged by work
          analysis over the last {TREND_WINDOW_DAYS} days.
        </p>
      </div>

      {!isLoading && !error && !scoresAvailable ? (
        <div className="border-border bg-card flex h-64 items-center justify-center rounded-lg border">
          <WidgetEmptyState message="No work analysis data yet. Sessions appear here once work analysis is enabled for your organization." />
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
          <WorkDoneChart
            buckets={buckets}
            loading={isLoading}
            error={Boolean(error)}
            expandedChart={expandedChart}
            onExpand={setExpandedChart}
          />
          <EfficiencyChart
            buckets={buckets}
            loading={isLoading}
            error={Boolean(error)}
            expandedChart={expandedChart}
            onExpand={setExpandedChart}
          />
        </div>
      )}
    </div>
  );
}
