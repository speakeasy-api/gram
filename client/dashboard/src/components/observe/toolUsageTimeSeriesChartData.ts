import { formatChartLabel } from "@/components/chart/chartUtils";
import {
  OUTCOME,
  otherSeriesForTheme,
  SERIES,
  seriesForTheme,
  withAlpha,
} from "@/components/chart/palette";
import type { ChartDataset } from "chart.js";

// The shared editorial series ramp (ink + stepped neutrals).
const DEFAULT_TIME_SERIES_COLORS = SERIES;

// Series beyond the palette fold into a single neutral "Other" bucket instead
// of cycling colors — two series sharing a hue are indistinguishable.
const OTHER_SERIES_LABEL = "Other";

export type TimeSeriesDataset = ChartDataset<"bar", number[]>;

const HOUR_MS = 3_600_000;
const DAY_MS = 24 * HOUR_MS;
const TIME_BUCKET_STEPS_MS = [
  HOUR_MS,
  3 * HOUR_MS,
  6 * HOUR_MS,
  12 * HOUR_MS,
  DAY_MS,
  2 * DAY_MS,
  7 * DAY_MS,
] as const;

// Sparse event counts need coarse-enough buckets to show a shape: ~48 bars max
// keeps a 7-day range at 6h buckets and a 90-day range at 2-day buckets.
const MAX_TIME_BUCKETS = 48;

export function pickTimeBucketMs(timeRangeMs: number): number {
  for (const step of TIME_BUCKET_STEPS_MS) {
    if (timeRangeMs / step <= MAX_TIME_BUCKETS) return step;
  }
  return TIME_BUCKET_STEPS_MS[TIME_BUCKET_STEPS_MS.length - 1]!;
}

export function bucketStartNsToMs(bucketStartNs: string): number | null {
  try {
    const ms = Number(BigInt(bucketStartNs) / 1_000_000n);
    return Number.isFinite(ms) ? ms : null;
  } catch {
    return null;
  }
}

function formatTooltipLabel(ts: number, bucketMs: number): string {
  const date = new Date(ts);
  if (bucketMs >= DAY_MS) {
    return date.toLocaleDateString([], { month: "short", day: "numeric" });
  }
  return date.toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

/**
 * Re-buckets raw time series points onto a dense, evenly spaced grid spanning
 * [from, to], anchored at `from`, and returns stacked-bar datasets ordered by
 * series total (largest first, so it sits at the bottom of the stack).
 *
 * The dense grid matters: a category axis only shows the labels it is given,
 * so charting just the buckets that contain data silently deletes the quiet
 * periods and distorts the time dimension.
 */
export function buildToolUsageTimeSeries<
  T extends { bucketStartNs: string; eventCount: number },
>(
  timeSeries: T[],
  keyFn: (p: T) => string,
  from: Date,
  to: Date,
  valueFn: (p: T) => number = (p) => p.eventCount,
  colors: readonly string[] = DEFAULT_TIME_SERIES_COLORS,
  otherColor?: string,
): {
  timestamps: number[];
  labels: string[];
  tooltipLabels: string[];
  datasets: TimeSeriesDataset[];
  bucketMs: number;
} {
  const fromMs = from.getTime();
  const toMs = to.getTime();
  const timeRangeMs = Math.max(toMs - fromMs, HOUR_MS);
  const bucketMs = pickTimeBucketMs(timeRangeMs);
  // +1 so a server bucket starting exactly at `to` still has a slot.
  const bucketCount = Math.floor(timeRangeMs / bucketMs) + 1;

  const seriesMap = new Map<string, number[]>();

  for (const pt of timeSeries) {
    const key = keyFn(pt);
    if (!key) continue;
    const ms = bucketStartNsToMs(pt.bucketStartNs);
    if (ms == null) continue;
    // Data is range-filtered server-side; clamp edge buckets (e.g. the bucket
    // containing `from`) into the grid rather than dropping their counts.
    const idx = Math.min(
      Math.max(Math.floor((ms - fromMs) / bucketMs), 0),
      bucketCount - 1,
    );
    const series =
      seriesMap.get(key) ?? Array.from({ length: bucketCount }, () => 0);
    series[idx] = (series[idx] ?? 0) + valueFn(pt);
    seriesMap.set(key, series);
  }

  if (seriesMap.size === 0) {
    return {
      timestamps: [],
      labels: [],
      tooltipLabels: [],
      datasets: [],
      bucketMs,
    };
  }

  const timestamps = Array.from(
    { length: bucketCount },
    (_, i) => fromMs + i * bucketMs,
  );
  const labels = timestamps.map((ts) =>
    formatChartLabel(new Date(ts), timeRangeMs),
  );
  const tooltipLabels = timestamps.map((ts) =>
    formatTooltipLabel(ts, bucketMs),
  );

  const sortedSeries = Array.from(seriesMap.entries())
    .map(([key, data]) => ({
      key,
      data,
      total: data.reduce((a, b) => a + b, 0),
    }))
    .sort((a, b) => b.total - a.total);

  // Fold overflow series into "Other" rather than cycling the palette.
  const visible =
    sortedSeries.length > colors.length
      ? sortedSeries.slice(0, colors.length - 1)
      : sortedSeries;
  const overflow = sortedSeries.slice(visible.length);
  if (overflow.length > 0) {
    const otherData = Array.from({ length: bucketCount }, () => 0);
    for (const series of overflow) {
      for (let i = 0; i < bucketCount; i++) {
        otherData[i] = (otherData[i] ?? 0) + (series.data[i] ?? 0);
      }
    }
    visible.push({
      key: OTHER_SERIES_LABEL,
      data: otherData,
      total: otherData.reduce((a, b) => a + b, 0),
    });
  }

  // The rollup neutral matching the ramp's theme: callers pass one of the two
  // stable seriesForTheme arrays (via useSeriesColors), so identity against
  // the dark ramp resolves the theme without threading a separate flag.
  // Custom ramps (e.g. [ACCENT_RED]) fall back to the light neutral.
  const rollupColor =
    otherColor ?? otherSeriesForTheme(colors === seriesForTheme(true));

  const datasets: TimeSeriesDataset[] = visible.map(({ key, data }, i) => {
    const isOther = overflow.length > 0 && i === visible.length - 1;
    const color = isOther
      ? rollupColor
      : (colors[i] ?? DEFAULT_TIME_SERIES_COLORS[0]!);
    return {
      label: key,
      data,
      backgroundColor: color,
      hoverBackgroundColor: withAlpha(color, 0.8),
      stack: "total",
      maxBarThickness: 32,
    };
  });

  return { timestamps, labels, tooltipLabels, datasets, bucketMs };
}

/** The parts of a series point that split its calls by outcome. */
export type ToolOutcomePoint = {
  bucketStartNs: string;
  eventCount: number;
  failureCount: number;
  blockedCount: number;
  pendingCount: number;
};

/**
 * Calls per bucket split by outcome, on the grid the caller has laid out:
 * succeeded, failed, blocked and pending, each in its OUTCOME color, so every
 * chart of the same calls paints the same outcome the same way. Order and
 * color are fixed per outcome, never by rank, and an outcome with no calls
 * gets no series (it would add a legend entry and tooltip line for nothing).
 */
export function toolOutcomeDatasets(
  timeSeries: ToolOutcomePoint[],
  grid: { fromMs: number; bucketMs: number; bucketCount: number },
  pendingColor: string,
): TimeSeriesDataset[] {
  const { fromMs, bucketMs, bucketCount } = grid;
  const zeros = () => Array.from<number>({ length: bucketCount }).fill(0);
  const ok = zeros();
  const failed = zeros();
  const blocked = zeros();
  const pending = zeros();
  for (const point of timeSeries) {
    const ms = bucketStartNsToMs(point.bucketStartNs);
    if (ms === null) continue;
    // The bucket containing `from` starts before it. Clamp rather than drop
    // it, or the first bar of every window silently loses its events.
    const index = Math.min(
      Math.max(Math.floor((ms - fromMs) / bucketMs), 0),
      bucketCount - 1,
    );
    const failures = Number(point.failureCount);
    const blocks = Number(point.blockedCount);
    const pends = Number(point.pendingCount);
    // eventCount is every call in the bucket, so the successful remainder is
    // what the three named outcomes leave behind.
    ok[index] =
      (ok[index] ?? 0) +
      Math.max(Number(point.eventCount) - failures - blocks - pends, 0);
    failed[index] = (failed[index] ?? 0) + failures;
    blocked[index] = (blocked[index] ?? 0) + blocks;
    pending[index] = (pending[index] ?? 0) + pends;
  }
  const series: [string, number[], string, string][] = [
    ["Succeeded", ok, OUTCOME.ok, withAlpha(OUTCOME.ok, 0.8)],
    ["Failed", failed, OUTCOME.failed, OUTCOME.failed],
    ["Blocked", blocked, OUTCOME.blocked, OUTCOME.blocked],
    ["Pending", pending, pendingColor, pendingColor],
  ];
  return series
    .filter(([, data]) => data.some((value) => value > 0))
    .map(([label, data, color, hover]) => ({
      label,
      data,
      backgroundColor: color,
      hoverBackgroundColor: hover,
      stack: "total",
      maxBarThickness: 32,
    }));
}

/**
 * Calls over time split by outcome, on the same dense grid and labels as
 * buildToolUsageTimeSeries.
 */
export function buildToolOutcomeTimeSeries(
  timeSeries: ToolOutcomePoint[],
  from: Date,
  to: Date,
  pendingColor: string,
): {
  timestamps: number[];
  labels: string[];
  tooltipLabels: string[];
  datasets: TimeSeriesDataset[];
  bucketMs: number;
} {
  const fromMs = from.getTime();
  const timeRangeMs = Math.max(to.getTime() - fromMs, HOUR_MS);
  const bucketMs = pickTimeBucketMs(timeRangeMs);
  const bucketCount = Math.floor(timeRangeMs / bucketMs) + 1;
  const datasets = toolOutcomeDatasets(
    timeSeries,
    { fromMs, bucketMs, bucketCount },
    pendingColor,
  );
  if (datasets.length === 0) {
    return {
      timestamps: [],
      labels: [],
      tooltipLabels: [],
      datasets: [],
      bucketMs,
    };
  }
  const timestamps = Array.from(
    { length: bucketCount },
    (_, i) => fromMs + i * bucketMs,
  );
  return {
    timestamps,
    labels: timestamps.map((ts) => formatChartLabel(new Date(ts), timeRangeMs)),
    tooltipLabels: timestamps.map((ts) => formatTooltipLabel(ts, bucketMs)),
    datasets,
    bucketMs,
  };
}
