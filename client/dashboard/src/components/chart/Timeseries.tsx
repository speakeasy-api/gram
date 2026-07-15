// The one zoomable timeseries chart component for the app. Expresses every
// shape the app currently hand-rolls across five separate components (three
// of which are independently named "TokenTimeSeriesChart"): a plain line, a
// filled area, stacked bars, and stacked bars with a smoothed trend line
// overlay (see ToolCallsTimeSeriesChart.tsx for the trend-line approach this
// mirrors). Styling — palette, tooltip, grid, ticks — comes from
// ./chart-theme so every timeseries chart in the app looks the same by
// construction instead of by convention.
import { formatChartLabel, smoothData } from "@/components/chart/chartUtils";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { formatCompact } from "@/lib/format";
import { ChartNoAxesColumn, RotateCcw } from "lucide-react";
import type { ChartDataset, ChartOptions, TooltipItem } from "chart.js";
import { useCallback, useMemo, useState } from "react";
import { Chart } from "react-chartjs-2";
import { ChartButton } from "./ChartButton";
import {
  chartGrid,
  chartTicks,
  chartTooltip,
  registerChartJs,
  seriesPalette,
  timeTitleFormatter,
  trendLineColor,
  withAlpha,
} from "./chart-theme";
import { useChartZoom } from "./useChartZoom";

registerChartJs();

export type TimeseriesPoint = {
  x: number | Date;
  y: number;
};

export type TimeseriesSeries = {
  label: string;
  data: TimeseriesPoint[];
  /** Overrides the palette color assigned by series index. */
  color?: string;
};

/** @public — part of the component's prop API. */
export type TimeseriesMode = "line" | "area" | "stacked-bar" | "bar-with-trend";

export type TimeseriesProps = {
  series: TimeseriesSeries[];
  mode?: TimeseriesMode;
  height?: number;
  valueFormatter?: (value: number) => string;
  /** Enables drag-to-zoom via the shared useChartZoom hook. */
  enableZoom?: boolean;
  onZoomRange?: (from: Date, to: Date) => void;
  emptyMessage?: string;
  loading?: boolean;
  /** Hides the HTML legend even when there's more than one series. */
  showLegend?: boolean;
};

type MixedPoint = { x: number; y: number | null };
type MixedDataset =
  | ChartDataset<"bar", MixedPoint[]>
  | ChartDataset<"line", MixedPoint[]>;

function toMillis(x: number | Date): number {
  return x instanceof Date ? x.getTime() : x;
}

// Sums every series' y value at each shared x, then smooths the result — the
// same centered-moving-average trend line ToolCallsTimeSeriesChart draws
// over its stacked success/fail bars, generalized to N series.
function buildTrendPoints(series: { data: MixedPoint[] }[]): MixedPoint[] {
  const totals = new Map<number, number>();
  for (const s of series) {
    for (const point of s.data) {
      if (typeof point.y !== "number") continue;
      totals.set(point.x, (totals.get(point.x) ?? 0) + point.y);
    }
  }
  const sortedX = Array.from(totals.keys()).sort((a, b) => a - b);
  const smoothed = smoothData(sortedX.map((x) => totals.get(x)!));
  return sortedX.map((x, i) => ({ x, y: smoothed[i] ?? null }));
}

export type ChartLegendEntry = {
  label: string;
  color: string;
};

/**
 * The chart system's HTML legend: square color dots identifying series,
 * rendered outside the canvas (canvas legends are disabled everywhere in
 * this system per the design language).
 */
export function ChartLegend({
  entries,
}: {
  entries: ChartLegendEntry[];
}): JSX.Element {
  return (
    <ul className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1.5">
      {entries.map((entry) => (
        <li
          key={entry.label}
          className="text-muted-foreground flex min-w-0 items-center gap-1.5 text-xs"
        >
          <span
            aria-hidden="true"
            className="size-2 shrink-0"
            style={{ backgroundColor: entry.color }}
          />
          <span className="truncate">{entry.label}</span>
        </li>
      ))}
    </ul>
  );
}

/** Shared empty state for charts in this system — a neutral badge, centered. */
export function ChartNoData({
  message = "No data in this period",
  height = 200,
}: {
  message?: string;
  height?: number;
}): JSX.Element {
  return (
    <div className="flex items-center justify-center" style={{ height }}>
      <Badge variant="neutral">
        <Badge.LeftIcon>
          <ChartNoAxesColumn className="size-4" />
        </Badge.LeftIcon>
        <Badge.Text>{message}</Badge.Text>
      </Badge>
    </div>
  );
}

export function Timeseries({
  series,
  mode = "line",
  height = 260,
  valueFormatter,
  enableZoom = false,
  onZoomRange,
  emptyMessage,
  loading = false,
  showLegend = true,
}: TimeseriesProps): JSX.Element {
  const [isZoomedLocal, setIsZoomedLocal] = useState(false);
  const isStacked = mode === "stacked-bar" || mode === "bar-with-trend";
  const hasData = series.some((s) => s.data.length > 0);

  const normalizedSeries = useMemo(
    () =>
      series.map((s) => ({
        label: s.label,
        color: s.color,
        data: s.data
          .map((p) => ({ x: toMillis(p.x), y: p.y }) satisfies MixedPoint)
          .sort((a, b) => a.x - b.x),
      })),
    [series],
  );

  const { datasets, legendEntries, timeRangeMs } = useMemo(() => {
    const palette = seriesPalette();
    const colorFor = (i: number, override?: string): string =>
      override ?? palette[i % palette.length]!;

    const allX: number[] = [];
    let built: MixedDataset[] = [];

    if (mode === "line" || mode === "area") {
      built = normalizedSeries.map((s, i) => {
        allX.push(...s.data.map((p) => p.x));
        const color = colorFor(i, s.color);
        return {
          type: "line",
          label: s.label,
          data: s.data,
          borderColor: color,
          backgroundColor:
            mode === "area" ? withAlpha(color, 0.16) : "transparent",
          pointRadius: 0,
          pointHoverRadius: 3,
          borderWidth: 2,
          tension: 0.3,
          fill: mode === "area",
          spanGaps: true,
        } satisfies ChartDataset<"line", MixedPoint[]>;
      });
    } else {
      const bars = normalizedSeries.map((s, i) => {
        allX.push(...s.data.map((p) => p.x));
        const color = colorFor(i, s.color);
        return {
          type: "bar",
          label: s.label,
          data: s.data,
          backgroundColor: withAlpha(color, 0.78),
          hoverBackgroundColor: color,
          stack: "stack",
          borderRadius: 0,
          borderSkipped: false,
          maxBarThickness: 28,
          order: 2,
        } satisfies ChartDataset<"bar", MixedPoint[]>;
      });

      built = bars;

      if (mode === "bar-with-trend") {
        const trend: ChartDataset<"line", MixedPoint[]> = {
          type: "line",
          label: "Trend",
          data: buildTrendPoints(normalizedSeries),
          borderColor: trendLineColor(),
          backgroundColor: "transparent",
          pointRadius: 0,
          pointHoverRadius: 3,
          borderWidth: 2,
          tension: 0.4,
          fill: false,
          order: 1,
        };
        built = [...bars, trend];
      }
    }

    const legendEntries: ChartLegendEntry[] = normalizedSeries.map((s, i) => ({
      label: s.label,
      color: colorFor(i, s.color),
    }));

    const minX = allX.length > 0 ? Math.min(...allX) : 0;
    const maxX = allX.length > 0 ? Math.max(...allX) : 0;

    return {
      datasets: built,
      legendEntries,
      timeRangeMs: Math.max(0, maxX - minX),
    };
  }, [normalizedSeries, mode]);

  const { chartRef, zoomPluginOptions, resetZoom } = useChartZoom<
    "bar" | "line",
    MixedPoint[],
    unknown
  >({
    onRangeSelect: enableZoom
      ? (from, to) => {
          setIsZoomedLocal(true);
          onZoomRange?.(from, to);
        }
      : undefined,
  });

  const handleResetZoom = useCallback(() => {
    resetZoom();
    setIsZoomedLocal(false);
  }, [resetZoom]);

  const formatValue = useCallback(
    (value: number): string =>
      valueFormatter ? valueFormatter(value) : formatCompact(value),
    [valueFormatter],
  );

  const options = useMemo<ChartOptions<"line">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      transitions: { resize: { animation: { duration: 0 } } },
      plugins: {
        legend: { display: false },
        tooltip: {
          ...chartTooltip(),
          callbacks: {
            title: timeTitleFormatter,
            label: (item: TooltipItem<"line">) => {
              const value = item.parsed.y;
              if (typeof value !== "number") return undefined;
              return ` ${item.dataset.label}: ${formatValue(value)}`;
            },
          },
        },
        zoom: zoomPluginOptions,
      },
      scales: {
        x: {
          type: "linear",
          stacked: isStacked,
          grid: chartGrid(),
          ticks: {
            ...chartTicks(),
            maxTicksLimit: 8,
            callback: (value) =>
              formatChartLabel(new Date(Number(value)), timeRangeMs),
          },
        },
        y: {
          stacked: isStacked,
          beginAtZero: true,
          grid: chartGrid(),
          ticks: {
            ...chartTicks(),
            callback: (value) => formatValue(Number(value)),
          },
        },
      },
    }),
    [isStacked, timeRangeMs, formatValue, zoomPluginOptions],
  );

  if (loading) {
    return <Skeleton className="w-full" style={{ height }} />;
  }

  if (!hasData) {
    return <ChartNoData message={emptyMessage} height={height} />;
  }

  return (
    <div className="w-full">
      {isZoomedLocal && (
        <div className="mb-2 flex justify-end">
          <ChartButton onClick={handleResetZoom} ariaLabel="Reset zoom">
            <RotateCcw className="size-4" />
            Reset zoom
          </ChartButton>
        </div>
      )}
      <div style={{ height }}>
        <Chart<"bar" | "line", MixedPoint[], unknown>
          ref={chartRef}
          type="line"
          data={{ datasets }}
          options={options}
        />
      </div>
      {showLegend && legendEntries.length > 1 && (
        <ChartLegend entries={legendEntries} />
      )}
    </div>
  );
}
