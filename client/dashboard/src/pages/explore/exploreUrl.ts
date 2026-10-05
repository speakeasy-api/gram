import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import {
  MAX_LIMIT,
  specProblem,
  type ExploreSpec,
  type FilterDraft,
  type MeasureDraft,
  type TimeRange,
  isChartType,
  isFilterOperator,
  isMeasureOp,
  isWindowPreset,
} from "./exploreModel";

// The URL carries the whole query, so any query is a link and sharing never
// requires saving it first. The spec travels as one JSON parameter: filter
// values are arbitrary text, and JSON carries them without an escaping
// scheme of its own.

/** The search parameter holding the query. */
export const QUERY_PARAM = "q";

/** The search parameter naming the widget the builder has open. */
export const WIDGET_PARAM = "widget";

/** The search parameter naming the page's tab; absent means Explore. */
export const TAB_PARAM = "tab";

// Bumped when the encoding changes shape; a link in an older shape then
// falls back to the default view rather than being misread.
const VERSION = 1;

/** The query as its URL parameter value. */
export function encodeSpec(spec: ExploreSpec): string {
  return JSON.stringify({
    v: VERSION,
    dataset: spec.dataset,
    measures: spec.measures.map(({ op, field }) => ({ op, field })),
    filters: spec.filters.map(({ field, operator, values }) => ({
      field,
      operator,
      values,
    })),
    dimensions: spec.dimensions,
    orderBy: spec.orderBy,
    limit: spec.limit,
    window: spec.window,
    // Only present when set, so a link with no range reads as it always did.
    ...(spec.range
      ? { range: { from: spec.range.from, to: spec.range.to } }
      : {}),
    chartType: spec.chartType,
  });
}

/**
 * The query a URL parameter carries, or null when there is none to restore:
 * nothing there, text that does not parse, or a query that names something
 * the catalog no longer has. Null means the default view, never an error —
 * a link outliving a catalog change should still open Explore.
 */
export function decodeSpec(
  raw: string | null,
  datasets: AnalyticsDataset[],
): ExploreSpec | null {
  const spec = parseSpec(raw);
  if (!spec || specProblem(datasets, spec) !== "") return null;
  return spec;
}

/**
 * The query a URL parameter carries, whether or not the catalog can still
 * answer it, or null when the text is not a query at all.
 */
export function parseSpec(raw: string | null): ExploreSpec | null {
  if (raw === null || raw === "") return null;
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return null;
  }
  return specFromValue(value);
}

function specFromValue(value: unknown): ExploreSpec | null {
  if (!isRecord(value) || value.v !== VERSION) return null;
  const {
    dataset,
    measures,
    filters,
    dimensions,
    orderBy,
    limit,
    window,
    range,
    chartType,
  } = value;
  if (typeof dataset !== "string" || dataset === "") return null;
  if (!isStringArray(dimensions)) return null;
  if (typeof orderBy !== "string") return null;
  if (!isWindowPreset(window) || !isChartType(chartType)) return null;
  if (
    typeof limit !== "number" ||
    !Number.isInteger(limit) ||
    limit < 0 ||
    limit > MAX_LIMIT
  ) {
    return null;
  }
  if (!Array.isArray(measures) || !Array.isArray(filters)) return null;
  const timeRange = range === undefined ? undefined : rangeFromValue(range);
  if (timeRange === null) return null;

  const measureDrafts: MeasureDraft[] = [];
  for (const measure of measures) {
    if (!isRecord(measure)) return null;
    if (!isMeasureOp(measure.op) || typeof measure.field !== "string") {
      return null;
    }
    measureDrafts.push({ op: measure.op, field: measure.field });
  }

  const filterDrafts: FilterDraft[] = [];
  for (const filter of filters) {
    if (!isRecord(filter)) return null;
    const { field, operator, values } = filter;
    if (typeof field !== "string" || !isFilterOperator(operator)) return null;
    if (!isStringArray(values)) return null;
    filterDrafts.push({ field, operator, values });
  }

  return {
    dataset,
    measures: measureDrafts,
    filters: filterDrafts,
    dimensions,
    orderBy,
    limit,
    window,
    ...(timeRange ? { range: timeRange } : {}),
    chartType,
  };
}

/** An absolute range: two whole milliseconds, from before to. */
function rangeFromValue(value: unknown): TimeRange | null {
  if (!isRecord(value)) return null;
  const { from, to } = value;
  if (
    typeof from !== "number" ||
    typeof to !== "number" ||
    !Number.isSafeInteger(from) ||
    !Number.isSafeInteger(to) ||
    from >= to
  ) {
    return null;
  }
  return { from, to };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isStringArray(value: unknown): value is string[] {
  return (
    Array.isArray(value) && value.every((item) => typeof item === "string")
  );
}
