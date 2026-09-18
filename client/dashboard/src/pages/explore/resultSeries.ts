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
}

export interface SeriesSet {
  /** Bucket starts, ISO 8601, ascending. */
  buckets: string[];
  series: Series[];
  /** Series dropped past MAX_SERIES, smallest totals first. */
  hidden: number;
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
 */
export function seriesFromRows(
  rows: ResultRow[],
  dimensions: string[],
  measures: MeasureDraft[],
  dataset: AnalyticsDataset | undefined,
): SeriesSet {
  const buckets = [
    ...new Set(rows.map((row) => textCell(row[TIME_BUCKET_COLUMN]))),
  ].sort();
  const bucketIndex = new Map(buckets.map((bucket, index) => [bucket, index]));

  const order: string[] = [];
  const points = new Map<string, (number | null)[]>();
  const units = new Map<string, string>();
  for (const row of rows) {
    const tuple = tupleLabel(row, dimensions);
    const at = bucketIndex.get(textCell(row[TIME_BUCKET_COLUMN]));
    for (const measure of measures) {
      const value = numericCell(row[measureAlias(measure)]);
      if (value === null || at === undefined) continue;
      const label = seriesLabel(measure, measures.length, tuple, dimensions);
      let series = points.get(label);
      if (!series) {
        series = buckets.map(() => null);
        points.set(label, series);
        units.set(label, measureUnit(dataset, measure));
        order.push(label);
      }
      series[at] = value;
    }
  }

  const ranked = order
    .map((label) => ({
      label,
      unit: units.get(label) ?? "",
      points: points.get(label) ?? [],
    }))
    .sort((a, b) => total(b.points) - total(a.points));

  return {
    buckets,
    series: ranked.slice(0, MAX_SERIES),
    hidden: Math.max(0, ranked.length - MAX_SERIES),
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
