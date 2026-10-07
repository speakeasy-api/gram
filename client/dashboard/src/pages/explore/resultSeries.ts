import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import {
  measureAlias,
  measureLabel,
  measureUnit,
  numericCell,
  textCell,
  TIME_BUCKET_COLUMN,
  type Grain,
  type MeasureDraft,
  type ResultRow,
} from "./exploreModel";

// Series drawn on one chart. Three dimensions of a few values each multiply
// quickly, and past this many lines nothing is readable anyway.
export const MAX_SERIES = 12;

interface Series {
  label: string;
  unit: string;
  /** One point per bucket, null where the series had no row. */
  points: (number | null)[];
  /**
   * The fold of every series past the cap, summed per bucket, so a stack's
   * height is the real total. Marked here rather than by label, so a
   * dimension value that happens to be called "Other" is never mistaken for
   * it.
   */
  other?: boolean;
}

export interface SeriesSet {
  /** Bucket starts, ISO 8601, ascending. */
  buckets: string[];
  series: Series[];
  /**
   * Series past MAX_SERIES, smallest totals first: dropped from the chart,
   * or, when folded, summed into the trailing Other series.
   */
  hidden: number;
}

/**
 * The label of the series the fold is drawn as. It says how many series it
 * holds, which also keeps it apart from a dimension value called "Other".
 */
export function otherLabel(count: number): string {
  return `Other (${count} series)`;
}

/** A row's dimension values as one label; the empty tuple reads as "all". */
export function tupleLabel(row: ResultRow, dimensions: string[]): string {
  if (dimensions.length === 0) return "all";
  return dimensions.map((dimension) => textCell(row[dimension])).join(" · ");
}

/**
 * Pivot bucketed rows into one series per (dimension tuple × measure). A
 * single measure names each series by its tuple; several measures name them
 * by measure, with the tuple appended when there is one.
 *
 * Past MAX_SERIES the smallest series are dropped, or, with `fold`, summed
 * into one trailing Other series: a line chart stays true with lines left
 * out, but a stack's height reads as the total, so a stack folds.
 */
export function seriesFromRows(
  rows: ResultRow[],
  dimensions: string[],
  measures: MeasureDraft[],
  dataset: AnalyticsDataset | undefined,
  fold = false,
): SeriesSet {
  const buckets = [
    ...new Set(rows.map((row) => textCell(row[TIME_BUCKET_COLUMN]))),
  ].sort();
  const bucketIndex = new Map(buckets.map((bucket, index) => [bucket, index]));

  // A series is keyed by its raw tuple and measure, not by its label: a
  // dimension value that itself contains the label's separator would
  // otherwise fold two tuples into one series.
  const order: string[] = [];
  const labels = new Map<string, string>();
  const points = new Map<string, (number | null)[]>();
  const units = new Map<string, string>();
  for (const row of rows) {
    const values = dimensions.map((dimension) => textCell(row[dimension]));
    const tuple = tupleLabel(row, dimensions);
    const at = bucketIndex.get(textCell(row[TIME_BUCKET_COLUMN]));
    for (const measure of measures) {
      const value = numericCell(row[measureAlias(measure)]);
      if (value === null || at === undefined) continue;
      const key = JSON.stringify([measureAlias(measure), ...values]);
      let series = points.get(key);
      if (!series) {
        series = buckets.map(() => null);
        points.set(key, series);
        labels.set(
          key,
          seriesLabel(measure, measures.length, tuple, dimensions),
        );
        units.set(key, measureUnit(dataset, measure));
        order.push(key);
      }
      series[at] = value;
    }
  }

  const ranked: Series[] = order
    .map((key) => ({
      label: labels.get(key) ?? "",
      unit: units.get(key) ?? "",
      points: points.get(key) ?? [],
    }))
    .sort((a, b) => total(b.points) - total(a.points));

  if (!fold || ranked.length <= MAX_SERIES) {
    return {
      buckets,
      series: ranked.slice(0, MAX_SERIES),
      hidden: Math.max(0, ranked.length - MAX_SERIES),
    };
  }

  // The named bands plus Other never exceed the cap, so a stack has no more
  // bands than a line chart has lines.
  const named = ranked.slice(0, MAX_SERIES - 1);
  const rest = ranked.slice(MAX_SERIES - 1);
  return {
    buckets,
    series: [...named, foldOf(rest, buckets.length)],
    hidden: rest.length,
  };
}

/**
 * One series summing the given ones per bucket; a bucket none of them had a
 * row for stays null, since nothing was there to add.
 */
function foldOf(folded: Series[], bucketCount: number): Series {
  const points: (number | null)[] = [];
  for (let at = 0; at < bucketCount; at++) {
    let sum: number | null = null;
    for (const one of folded) {
      const point = one.points[at];
      if (point === null || point === undefined) continue;
      sum = (sum ?? 0) + point;
    }
    points.push(sum);
  }
  return {
    label: otherLabel(folded.length),
    unit: folded[0]?.unit ?? "",
    points,
    other: true,
  };
}

function seriesLabel(
  measure: MeasureDraft,
  measureCount: number,
  tuple: string,
  dimensions: string[],
): string {
  if (measureCount === 1) return tuple;
  if (dimensions.length === 0) return measureLabel(measure);
  return `${measureLabel(measure)} · ${tuple}`;
}

function total(points: (number | null)[]): number {
  let sum = 0;
  for (const point of points) sum += point ?? 0;
  return sum;
}

/** Whether the measures can share one axis: they all carry the same unit. */
export function sharedUnit(
  dataset: AnalyticsDataset | undefined,
  measures: MeasureDraft[],
): string | null {
  const units = new Set(
    measures.map((measure) => measureUnit(dataset, measure)),
  );
  if (units.size > 1) return null;
  return [...units][0] ?? "";
}

/**
 * Axis tick for a bucket start. Daily and coarser grains are dates; finer
 * grains are times, except a local-midnight bucket, which reads as the date
 * so the day boundaries stand out.
 */
export function bucketTick(iso: string, grain: Grain): string {
  const date = new Date(iso);
  const atMidnight = date.getHours() === 0 && date.getMinutes() === 0;
  if (isDailyOrCoarser(grain) || atMidnight) {
    return date.toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
    });
  }
  return date.toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

/** The full bucket start for a tooltip title, since ticks are sparse. */
export function bucketTitle(iso: string, grain: Grain): string {
  const date = new Date(iso);
  if (isDailyOrCoarser(grain)) {
    return date.toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
    });
  }
  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

function isDailyOrCoarser(grain: Grain): boolean {
  return grain === "day" || grain === "week" || grain === "month";
}

// A line through fewer points than this is not a line: a single bucket draws
// nothing at all. Such series show their points instead.
const SPARSE_POINTS = 3;

/** Whether a series has too few points to read as a line. */
export function isSparse(points: (number | null)[]): boolean {
  let present = 0;
  for (const point of points) {
    if (point !== null) present += 1;
  }
  return present <= SPARSE_POINTS;
}
