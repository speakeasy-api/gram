import { ChartCard } from "@/components/chart/ChartCard";
import { Page } from "@/components/page-layout";
import { GOOD_GREEN, OTHER_SERIES, TOOLTIP } from "@/components/chart/palette";
import { useSeriesColors } from "@/components/chart/useSeriesColors";
import { smoothData } from "@/components/chart/chartUtils";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { RequireScope } from "@/components/require-scope";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { useOrgMemoryDeveloperToggle } from "@/hooks/useOrgMemoryDeveloperToggle";
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
import { BusinessMemoryCorpus } from "./BusinessMemoryCorpus";

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

// Buckets are UTC days, so labels must be formatted in UTC: the shared
// formatChartLabel uses the local timezone, which shifts viewers west of UTC
// onto the previous date.
function formatTrendLabel(timestamp: string | Date): string {
  return new Date(timestamp).toLocaleDateString([], {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });
}

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
  const seriesColors = useSeriesColors();

  const chartData = useMemo<{
    labels: string[];
    datasets: Array<
      ChartDataset<"bar", number[]> | ChartDataset<"line", number[]>
    >;
  }>(() => {
    const labels = buckets.map((b) => formatTrendLabel(b.timestamp));
    const bars: ChartDataset<"bar", number[]> = {
      label: "Work delivered",
      data: buckets.map((b) => b.workUnits),
      // Lightest neutral so the bars recede behind the ink trend line.
      backgroundColor: OTHER_SERIES,
      order: 2,
    };
    const trend: ChartDataset<"line", number[]> = {
      label: "Trend",
      data: smoothData(buckets.map((b) => b.workUnits)),
      type: "line",
      // Ink from the theme-resolved editorial chart palette.
      borderColor: seriesColors[0]!,
      backgroundColor: "transparent",
      pointRadius: 0,
      pointHoverRadius: 4,
      borderWidth: 2,
      tension: 0.4,
      fill: false,
      order: 1,
    };
    return { labels, datasets: [bars, trend] };
  }, [buckets, seriesColors]);

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
          ...TOOLTIP,
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
  const seriesColors = useSeriesColors();

  const chartData = useMemo<{
    labels: string[];
    datasets: Array<ChartDataset<"line", Array<number | null>>>;
  }>(() => {
    const labels = buckets.map((b) => formatTrendLabel(b.timestamp));
    return {
      labels,
      datasets: [
        {
          label: "Cost efficiency",
          data: buckets.map((b) => b.costPerUnit ?? null),
          // Warm neutral from the theme-resolved editorial series ramp.
          borderColor: seriesColors[1]!,
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
          borderColor: GOOD_GREEN,
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
  }, [buckets, seriesColors]);

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
          ...TOOLTIP,
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

function OrgMemoryContent(): JSX.Element {
  const [isOrgMemoryEnabled] = useOrgMemoryDeveloperToggle();
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
      // Don't fire the fetch for users the redirect below is about to bounce.
      enabled: isOrgMemoryEnabled,
    },
  );

  if (!isOrgMemoryEnabled) {
    return <Navigate to={routes.home.href()} replace />;
  }

  const buckets = data?.buckets ?? [];
  const scoresAvailable = data?.scoresAvailable ?? false;

  return (
    <div className="h-full w-full overflow-y-auto">
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 p-6 pb-24">
        <div>
          <Page.Eyebrow className="mb-1" />
          <div className="flex items-center gap-2">
            <h1 className="text-display-sm font-thin">Org Memory</h1>
            <ReleaseStageBadge stage="preview" />
          </div>
          <p className="text-muted-foreground mt-1 text-sm">
            How much work your agents deliver and what it costs, judged by work
            analysis over the last {TREND_WINDOW_DAYS} days.
          </p>
        </div>

        {!isLoading && !error && !scoresAvailable ? (
          <div className="border-border bg-card flex h-64 items-center justify-center border">
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

        <BusinessMemoryCorpus />
      </div>
    </div>
  );
}

export default function OrgMemory(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <OrgMemoryContent />
    </RequireScope>
  );
}
