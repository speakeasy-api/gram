import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import {
  CHART_TYPE_OPTIONS,
  FILTER_OPERATOR_LABELS,
  MAX_LIMIT,
  specProblem,
  WINDOW_OPTIONS,
  type ChartType,
  type ExploreSpec,
  type FilterDraft,
  type FilterOperator,
  type MeasureDraft,
  type MeasureOp,
  type WindowPreset,
} from "./exploreModel";

// The URL carries the whole query, so any query is a link and sharing never
// requires saving it first. The spec travels as one JSON parameter: filter
// values are arbitrary text, and JSON carries them without an escaping
// scheme of its own.

/** The search parameter holding the query. */
export const QUERY_PARAM = "q";

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
  if (raw === null || raw === "") return null;
  let value: unknown;
  try {
    value = JSON.parse(raw);
  } catch {
    return null;
  }
  const spec = specFromValue(value);
  if (!spec || specProblem(datasets, spec) !== "") return null;
  return spec;
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
    chartType,
  } = value;
  if (typeof dataset !== "string" || dataset === "") return null;
  if (!isStringArray(dimensions)) return null;
  if (typeof orderBy !== "string") return null;
  if (!isWindow(window) || !isChartType(chartType)) return null;
  if (
    typeof limit !== "number" ||
    !Number.isInteger(limit) ||
    limit < 0 ||
    limit > MAX_LIMIT
  ) {
    return null;
  }
  if (!Array.isArray(measures) || !Array.isArray(filters)) return null;

  const measureDrafts: MeasureDraft[] = [];
  for (const measure of measures) {
    if (!isRecord(measure)) return null;
    if (typeof measure.op !== "string" || typeof measure.field !== "string") {
      return null;
    }
    measureDrafts.push({ op: measure.op as MeasureOp, field: measure.field });
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
    chartType,
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isStringArray(value: unknown): value is string[] {
  return (
    Array.isArray(value) && value.every((item) => typeof item === "string")
  );
}

function isWindow(value: unknown): value is WindowPreset {
  return WINDOW_OPTIONS.some((option) => option.value === value);
}

function isChartType(value: unknown): value is ChartType {
  return CHART_TYPE_OPTIONS.some((option) => option.value === value);
}

function isFilterOperator(value: unknown): value is FilterOperator {
  return typeof value === "string" && value in FILTER_OPERATOR_LABELS;
}
