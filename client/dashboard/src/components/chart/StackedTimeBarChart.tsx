import { ChartNoData } from "@/components/chart/ChartNoData";
import {
  CHART_COLORS,
  EXPANDED_LEGEND,
  SHARED_LEGEND,
  SHARED_RESIZE_TRANSITION,
  SHARED_TOOLTIP,
} from "@/components/chart/chartTheme";
import { useChartZoom } from "@/components/chart/useChartZoom";
import type { TimeSeriesDataset } from "@/components/observe/toolUsageTimeSeriesChartData";
import type { ChartOptions } from "chart.js";
import { useEffect } from "react";
import { Bar } from "react-chartjs-2";

export function StackedTimeBarChart({
  labels,
  timestamps,
  bucketMs,
  tooltipLabels,
  datasets,
  tooltipAfterBody,
  onRangeSelect,
  height = 200,
  expanded = false,
}: {
  labels: string[];
  timestamps: number[];
  bucketMs: number;
  tooltipLabels: string[];
  datasets: TimeSeriesDataset[];
  tooltipAfterBody?: (dataIndex: number) => string[];
  onRangeSelect?: (from: Date, to: Date) => void;
  height?: number;
  expanded?: boolean;
}) {
  const { chartRef, zoomPluginOptions, resetZoom } = useChartZoom<"bar">({
    onRangeSelect,
    resolveRange: (min, max) => {
      if (timestamps.length === 0) return null;
      const fromIndex = Math.max(0, Math.floor(min));
      const toIndex = Math.min(timestamps.length - 1, Math.ceil(max));
      const from = timestamps[fromIndex];
      const to = timestamps[toIndex];
      if (from == null || to == null) return null;
      // `to` is a bucket start; extend by the bucket width so the selection
      // covers the last bucket's events.
      return { from: new Date(from), to: new Date(to + bucketMs) };
    },
  });

  useEffect(() => {
    resetZoom();
  }, [datasets, resetZoom]);

  if (labels.length === 0) {
    return <ChartNoData />;
  }

  const options: ChartOptions<"bar"> = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: "index", intersect: false },
    plugins: {
      legend: expanded ? EXPANDED_LEGEND : SHARED_LEGEND,
      tooltip: {
        ...SHARED_TOOLTIP,
        // Index-mode hover activates every series at that x. Drop zeros so a
        // sparse multi-series chart doesn't build a tooltip listing every
        // inactive source (which balloons until it covers the chart). Returning
        // `undefined` from `label` is not enough — Chart.js treats that as
        // "use the default callback" and still renders the line.
        filter: (item) => (item.parsed.y ?? 0) !== 0,
        callbacks: {
          title: (items) => tooltipLabels[items[0]?.dataIndex ?? 0] ?? "",
          label: (item) =>
            item.formattedValue
              ? `${item.dataset.label}: ${item.formattedValue}`
              : "",
          ...(tooltipAfterBody
            ? {
                afterBody: (items) =>
                  tooltipAfterBody(items[0]?.dataIndex ?? 0),
              }
            : {}),
        },
      },
      zoom: zoomPluginOptions,
    },
    scales: {
      x: {
        stacked: true,
        grid: { display: false },
        ticks: { maxTicksLimit: 8, color: CHART_COLORS.labelFaded },
      },
      y: {
        stacked: true,
        beginAtZero: true,
        grid: { color: "rgba(128, 128, 128, 0.2)" },
        ticks: { precision: 0, color: CHART_COLORS.labelFaded },
      },
    },
    transitions: SHARED_RESIZE_TRANSITION,
  };

  return (
    <div
      className="relative transition-all duration-200 ease-in-out"
      style={{ height }}
    >
      <Bar ref={chartRef} data={{ labels, datasets }} options={options} />
    </div>
  );
}
