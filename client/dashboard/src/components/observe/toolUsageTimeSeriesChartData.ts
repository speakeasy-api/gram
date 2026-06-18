import { formatChartLabel } from "@/components/chart/chartUtils";
import type { ChartDataset } from "chart.js";

const DEFAULT_TIME_SERIES_COLORS = [
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

export type TimeSeriesDataset = ChartDataset<"line", number[]>;

export function buildToolUsageTimeSeries<
  T extends { bucketStartNs: string; eventCount: number },
>(
  timeSeries: T[],
  keyFn: (p: T) => string,
  timeRangeMs: number,
  valueFn: (p: T) => number = (p) => p.eventCount,
  colors: readonly string[] = DEFAULT_TIME_SERIES_COLORS,
): {
  timestamps: number[];
  labels: string[];
  tooltipLabels: string[];
  datasets: TimeSeriesDataset[];
} {
  if (timeSeries.length === 0) {
    return { timestamps: [], labels: [], tooltipLabels: [], datasets: [] };
  }

  const seriesMap = new Map<string, Map<number, number>>();

  for (const pt of timeSeries) {
    const key = keyFn(pt);
    if (!key) continue;
    const ms = Number(BigInt(pt.bucketStartNs) / BigInt(1_000_000));
    const series = seriesMap.get(key) ?? new Map<number, number>();
    series.set(ms, (series.get(ms) ?? 0) + valueFn(pt));
    seriesMap.set(key, series);
  }

  if (seriesMap.size === 0) {
    return { timestamps: [], labels: [], tooltipLabels: [], datasets: [] };
  }

  const allTimestamps = new Set<number>();
  for (const series of seriesMap.values()) {
    for (const ts of series.keys()) allTimestamps.add(ts);
  }
  const timestamps = Array.from(allTimestamps).sort((a, b) => a - b);
  const labels = timestamps.map((ts) =>
    formatChartLabel(new Date(ts), timeRangeMs),
  );
  const tooltipLabels = timestamps.map((ts) =>
    new Date(ts).toLocaleString([], {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    }),
  );

  const datasets: TimeSeriesDataset[] = Array.from(seriesMap.entries()).map(
    ([key, series], i) => {
      const color = colors[i % colors.length] ?? DEFAULT_TIME_SERIES_COLORS[0]!;
      return {
        label: key,
        data: timestamps.map((ts) => series.get(ts) ?? 0),
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

  return { timestamps, labels, tooltipLabels, datasets };
}
