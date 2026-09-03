import { ChartCard } from "@/components/chart/ChartCard";
import {
  formatChartLabel,
  smoothData,
  unixNanoToDate,
  unixNanoToMs,
} from "@/components/chart/chartUtils";
import {
  ACCENT_RED,
  AXIS,
  TOOLTIP,
  withAlpha,
} from "@/components/chart/palette";
import { useChartZoom } from "@/components/chart/useChartZoom";
import {
  useIsDarkTheme,
  useSeriesColors,
} from "@/components/chart/useSeriesColors";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { formatCompact } from "@/lib/format";
import type { TimeSeriesBucket } from "@gram/client/models/components/timeseriesbucket.js";
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
import ZoomPlugin from "chartjs-plugin-zoom";
import { useEffect, useMemo } from "react";
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
  ZoomPlugin,
);

export interface ToolCallsTimeSeriesChartProps {
  title: string;
  chartId: string;
  timeSeries: TimeSeriesBucket[];
  // Span of the selected window in milliseconds, used to pick the axis label format.
  timeRangeMs: number;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
  // Drag-to-select on the x axis. Receives the bucket-aligned window; the
  // caller applies it as the page's time range. Omit to leave the chart static.
  onRangeSelect?: (from: Date, to: Date) => void;
  isZoomed?: boolean;
  onResetZoom?: () => void;
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
  onRangeSelect,
  isZoomed,
  onResetZoom,
}: ToolCallsTimeSeriesChartProps): JSX.Element {
  const isExpanded = expandedChart === chartId;
  const height = isExpanded ? 420 : 260;
  const hasData = timeSeries.some((b) => b.totalToolCalls > 0);

  const timestamps = useMemo(
    () => timeSeries.map((b) => unixNanoToMs(b.bucketTimeUnixNano)),
    [timeSeries],
  );
  // Category axis: the plugin reports index bounds, not times.
  const { chartRef, zoomPluginOptions, resetZoom } = useChartZoom<
    "bar" | "line",
    number[],
    string
  >({
    onRangeSelect,
    resolveRange: (min, max) => {
      if (timestamps.length === 0) return null;
      const fromIndex = Math.max(0, Math.floor(min));
      const toIndex = Math.min(timestamps.length - 1, Math.ceil(max));
      const from = timestamps[fromIndex];
      const to = timestamps[toIndex];
      if (from == null || to == null) return null;
      const bucketMs =
        timestamps.length > 1
          ? timestamps[1]! - timestamps[0]!
          : Math.max(timeRangeMs, 60_000);
      // `to` is a bucket start; extend by the bucket width so the selection
      // covers the last bucket's events.
      return { from: new Date(from), to: new Date(to + bucketMs) };
    },
  });
  useEffect(() => {
    resetZoom();
  }, [timeSeries, resetZoom]);

  const seriesColors = useSeriesColors();
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

    // Editorial split: success is the quiet norm (light neutral); failure is
    // the one thing that earns the red accent.
    const barDatasets: Array<ChartDataset<"bar", number[]>> = [
      {
        label: "Successful",
        data: successData,
        // The ramp's neutral tail — quiet on both canvases.
        backgroundColor: withAlpha(seriesColors[8]!, 0.6),
        stack: "stack",
        order: 2,
      },
      {
        label: "Failed",
        data: failedData,
        backgroundColor: ACCENT_RED,
        stack: "stack",
        order: 2,
      },
    ];

    const trendDataset: ChartDataset<"line", number[]> = {
      label: "Total Trend",
      data: smoothData(timeSeries.map((b) => b.totalToolCalls)),
      type: "line",
      borderColor: seriesColors[0]!, // ink
      backgroundColor: "transparent",
      pointRadius: 0,
      pointHoverRadius: 4,
      borderWidth: 2,
      tension: 0.4,
      fill: false,
      order: 1,
    };

    return { labels, datasets: [...barDatasets, trendDataset] };
  }, [timeSeries, timeRangeMs, seriesColors]);

  // Chart.js paints the canvas with static defaults that ignore the CSS
  // theme, so gridlines and tick labels need explicit dark-mode colors.
  const isDark = useIsDarkTheme();

  const options = useMemo<ChartOptions<"bar">>(() => {
    const textColor = isDark ? AXIS.faded : AXIS.label;
    return {
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
            // Keep the trend line rendered on the chart, just don't clutter
            // the legend with a redundant "Total Trend" entry.
            filter: (legendItem) => legendItem.text !== "Total Trend",
          },
        },
        tooltip: {
          ...TOOLTIP,
          padding: 12,
          usePointStyle: true,
          callbacks: {
            label: (item) =>
              ` ${item.dataset.label}: ${formatCompact(Number(item.parsed.y ?? 0))}`,
          },
        },
        zoom: zoomPluginOptions,
      },
      scales: {
        x: {
          stacked: true,
          grid: {
            display: true,
            color: isDark ? AXIS.gridDark : AXIS.grid,
          },
          ticks: { maxTicksLimit: 8, color: textColor },
        },
        y: {
          stacked: true,
          beginAtZero: true,
          grid: { color: isDark ? AXIS.gridDark : AXIS.grid },
          ticks: {
            color: textColor,
            callback: (value) => formatCompact(Number(value)),
          },
        },
      },
    };
  }, [isDark, zoomPluginOptions]);

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasData}
      isZoomed={isZoomed}
      onResetZoom={onResetZoom}
    >
      {!hasData ? (
        <WidgetEmptyState
          message="No tool calls for the selected time range"
          className="h-[260px]"
        />
      ) : (
        <div style={{ height }}>
          {/* `<Chart>` (not `<Bar>`) because this mixes a stacked bar series
              with a line trend overlay; the explicit generic widens the
              accepted dataset union. */}
          <Chart<"bar" | "line", number[], string>
            ref={chartRef}
            type="bar"
            data={chartData}
            options={options}
          />
        </div>
      )}
    </ChartCard>
  );
}
