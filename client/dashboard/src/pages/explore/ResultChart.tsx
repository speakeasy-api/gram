import { AXIS, TOOLTIP, withAlpha } from "@/components/chart/palette";
import {
  useOtherSeriesColor,
  useSeriesColors,
} from "@/components/chart/useSeriesColors";
import { useIsDarkTheme } from "@/lib/theme";
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
  type TooltipItem,
} from "chart.js";
import { useChartZoom } from "@/components/chart/useChartZoom";
import ZoomPlugin from "chartjs-plugin-zoom";
import { useEffect, useMemo, type JSX } from "react";
import { Chart } from "react-chartjs-2";
import {
  formatMeasureValue,
  isStacked,
  type ChartType,
  type Grain,
} from "./exploreModel";
import {
  bucketTick,
  bucketTitle,
  isSparse,
  type SeriesSet,
} from "./resultSeries";

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

/** The chart's height in the Explore results panel. */
export const CHART_HEIGHT = 320;

type TimeseriesDataset = ChartDataset<"bar" | "line", (number | null)[]>;

/**
 * A bucketed result as a line, area or bar chart, one series per tuple, or
 * as a stack of them, one band per tuple.
 */
export function ResultChart({
  seriesSet,
  unit,
  chartType,
  grain,
  height,
  onRangeSelect,
}: {
  seriesSet: SeriesSet;
  /** The unit every series shares; the axis and tooltips format by it. */
  unit: string;
  chartType: ChartType;
  grain: Grain;
  /** Height in pixels; without one the chart fills its container. */
  height?: number;
  /** Dragging across the chart selects the buckets under the drag. */
  onRangeSelect?: ((from: Date, to: Date) => void) | undefined;
}): JSX.Element {
  const colors = useSeriesColors();
  const otherColor = useOtherSeriesColor();
  const isDark = useIsDarkTheme();
  const { buckets, series } = seriesSet;
  const stacked = isStacked(chartType);
  const bars = chartType === "bar" || chartType === "stacked_bar";

  // The x axis is categories, so the plugin reports bucket indexes; the
  // range runs from the first bucket's start to the last one's end.
  const { chartRef, zoomPluginOptions, resetZoom } = useChartZoom<
    "bar" | "line",
    (number | null)[],
    string
  >({
    onRangeSelect,
    resolveRange: (min, max) => {
      const first = buckets[Math.max(0, Math.floor(min))];
      const last = buckets[Math.min(buckets.length - 1, Math.ceil(max))];
      if (first === undefined || last === undefined) return null;
      const end = Date.parse(last) + bucketMs(grain);
      return { from: new Date(first), to: new Date(end) };
    },
  });
  // The narrowed answer arrives as new buckets; the drag box goes with the
  // old ones. The array is rebuilt on every render, so the reset is keyed on
  // the buckets themselves, or any re-render would drop the drag box.
  const bucketKey = buckets.join(",");
  useEffect(() => {
    resetZoom();
  }, [bucketKey, resetZoom]);

  const datasets = useMemo<TimeseriesDataset[]>(
    () =>
      series.map((one) => {
        const color = one.other
          ? otherColor
          : colors[paletteIndex(one.label, colors.length)]!;
        // A stacked band contributes nothing where its series had no row;
        // Chart.js breaks a stacked line at a null and leaves a bar gap, so
        // a stack draws the missing bucket as zero.
        const data = stacked
          ? one.points.map((point) => point ?? 0)
          : one.points;
        const base = { label: one.label, data, borderColor: color };
        if (bars) return { ...base, type: "bar", backgroundColor: color };
        if (chartType === "stacked_area") {
          // Each band fills down to the one below it, and the first to the
          // axis; the bands never overlap, so each is painted solid.
          return {
            ...base,
            type: "line",
            backgroundColor: color,
            borderWidth: 1,
            pointRadius: 0,
            pointHoverRadius: 4,
            fill: "stack",
          };
        }
        return {
          ...base,
          type: "line",
          backgroundColor: chartType === "area" ? withAlpha(color, 0.2) : color,
          borderWidth: 1.5,
          pointRadius: isSparse(one.points) ? 4 : 0,
          pointHoverRadius: 4,
          fill: chartType === "area",
          spanGaps: true,
        };
      }),
    [series, chartType, stacked, bars, colors, otherColor],
  );

  const options = useMemo<ChartOptions<"line" | "bar">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        zoom: zoomPluginOptions,
        legend: {
          display: datasets.length > 1,
          position: "bottom",
          labels: { color: AXIS.label, boxWidth: 10, boxHeight: 10 },
        },
        tooltip: {
          ...TOOLTIP,
          // A stacked bucket lists only the bands that contributed, and
          // what they add up to.
          filter: (item) => !stacked || (item.parsed.y ?? 0) !== 0,
          callbacks: {
            title: (items) => {
              const iso = items[0]?.label;
              return iso ? bucketTitle(iso, grain) : "";
            },
            label: (context) =>
              `${context.dataset.label}: ${formatMeasureValue(Number(context.parsed.y ?? 0), unit)}`,
            footer: (items) => (stacked ? stackTotal(items, unit) : ""),
          },
        },
      },
      scales: {
        x: {
          stacked,
          grid: { display: false },
          ticks: {
            color: AXIS.label,
            autoSkip: true,
            maxRotation: 0,
            maxTicksLimit: 10,
            callback: (value) => {
              const iso = buckets[Number(value)];
              return iso === undefined ? "" : bucketTick(iso, grain);
            },
          },
        },
        y: {
          stacked,
          beginAtZero: true,
          grid: { color: isDark ? AXIS.gridDark : AXIS.grid },
          ticks: {
            color: AXIS.label,
            callback: (value) => formatMeasureValue(Number(value), unit),
          },
        },
      },
    }),
    [datasets.length, unit, isDark, buckets, grain, zoomPluginOptions, stacked],
  );

  return (
    <div
      className={height === undefined ? "h-full min-h-0" : undefined}
      style={height === undefined ? undefined : { height }}
    >
      <Chart<"bar" | "line", (number | null)[], string>
        ref={chartRef}
        type={bars ? "bar" : "line"}
        data={{ labels: buckets, datasets }}
        options={options}
      />
    </div>
  );
}

/** The tooltip's last line on a stack: what the bands under the cursor add up to. */
function stackTotal(
  items: TooltipItem<"bar" | "line">[],
  unit: string,
): string {
  let sum = 0;
  for (const item of items) sum += Number(item.parsed.y ?? 0);
  return `Total: ${formatMeasureValue(sum, unit)}`;
}

const HOUR_MS = 3_600_000;

/** How long one bucket of a grain lasts; a month is taken as 31 days. */
function bucketMs(grain: Grain): number {
  switch (grain) {
    case "day":
      return 24 * HOUR_MS;
    case "week":
      return 7 * 24 * HOUR_MS;
    case "month":
      return 31 * 24 * HOUR_MS;
    case "hour":
    case "none":
      return HOUR_MS;
  }
}

/**
 * The palette slot a series keeps. Results reorder by total as a query is
 * refined, so an index would hand the same tuple a different colour and read
 * as a different series. Hashing the label pins it instead.
 */
function paletteIndex(label: string, count: number): number {
  let hash = 2166136261;
  for (let i = 0; i < label.length; i++) {
    hash ^= label.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  return Math.abs(hash) % count;
}
