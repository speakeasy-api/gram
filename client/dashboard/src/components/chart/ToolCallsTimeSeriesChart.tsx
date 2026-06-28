import { ChartCard } from "@/components/chart/ChartCard";
import {
  formatChartLabel,
  smoothData,
  unixNanoToDate,
} from "@/components/chart/chartUtils";
import { formatCompact } from "@/lib/format";
import type { TimeSeriesBucket } from "@gram/client/models/components";
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
import { useMemo } from "react";
import { Chart } from "react-chartjs-2";

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

export interface ToolCallsTimeSeriesChartProps {
  title: string;
  chartId: string;
  timeSeries: TimeSeriesBucket[];
  // Span of the selected window in milliseconds, used to pick the axis label format.
  timeRangeMs: number;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}

/**
 * Tool-call volume over time: stacked bars split successful vs failed calls,
 * with a smoothed trend line of total calls overlaid. Driven by the
 * `time_series` buckets returned from `getObservabilityOverview`.
 */
export function ToolCallsTimeSeriesChart({
  title,
  chartId,
  timeSeries,
  timeRangeMs,
  expandedChart,
  onExpand,
}: ToolCallsTimeSeriesChartProps): JSX.Element {
  const isExpanded = expandedChart === chartId;
  const height = isExpanded ? 420 : 260;
  const hasData = timeSeries.some((b) => b.totalToolCalls > 0);

  const chartData = useMemo<{
    labels: string[];
    datasets: Array<
      ChartDataset<"bar", number[]> | ChartDataset<"line", number[]>
    >;
  }>(() => {
    const labels = timeSeries.map((b) =>
      formatChartLabel(unixNanoToDate(b.bucketTimeUnixNano), timeRangeMs),
    );

    const successData = timeSeries.map((b) =>
      Math.max(b.totalToolCalls - b.failedToolCalls, 0),
    );
    const failedData = timeSeries.map((b) => b.failedToolCalls);

    const barDatasets: Array<ChartDataset<"bar", number[]>> = [
      {
        label: "Successful",
        data: successData,
        backgroundColor: "rgba(52, 211, 153, 0.35)",
        stack: "stack",
        order: 2,
      },
      {
        label: "Failed",
        data: failedData,
        backgroundColor: "rgba(248, 113, 113, 0.45)",
        stack: "stack",
        order: 2,
      },
    ];

    const trendDataset: ChartDataset<"line", number[]> = {
      label: "Total Trend",
      data: smoothData(timeSeries.map((b) => b.totalToolCalls)),
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

    return { labels, datasets: [...barDatasets, trendDataset] };
  }, [timeSeries, timeRangeMs]);

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
          stacked: true,
          grid: { display: true, color: "rgba(128, 128, 128, 0.08)" },
          ticks: { maxTicksLimit: 8 },
        },
        y: {
          stacked: true,
          beginAtZero: true,
          grid: { color: "rgba(128, 128, 128, 0.15)" },
          ticks: {
            callback: (value) => formatCompact(Number(value)),
          },
        },
      },
    }),
    [],
  );

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasData}
    >
      {!hasData ? (
        <div className="text-muted-foreground flex h-[260px] items-center justify-center text-sm">
          No tool calls for the selected time range
        </div>
      ) : (
        <div style={{ height }}>
          {/* `<Chart>` (not `<Bar>`) because this mixes a stacked bar series
              with a line trend overlay; the explicit generic widens the
              accepted dataset union. */}
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
