import {
  autoGrain,
  completeFilters,
  completeMeasures,
  hasChartShape,
  MAX_DIMENSIONS,
  MAX_LIMIT,
  measureAlias,
  type ExploreSpec,
  type FilterDraft,
  type MeasureDraft,
  type MeasureOp,
  isChartType,
  isFilterOperator,
  isWindowPreset,
} from "./exploreModel";

// A saved query keeps the builder state in the shape the explore service
// plans against the catalog (server/internal/explore/spec.go): the question
// in catalog vocabulary — snake_case, measures with their aliases, order as
// a list — plus the window and chart type only the client interprets. Rows
// still being composed are not part of the question, so they are not saved.

/** The spec a saved query stores for the builder's state. */
export function savedSpecFromSpec(spec: ExploreSpec): Record<string, unknown> {
  const measures = completeMeasures(spec.measures);
  const rows = measures.length === 0;
  const aliases = measures.map(measureAlias);
  return {
    chart_type: spec.chartType,
    window: spec.window,
    // Mode and grain follow from the rest: no measure means rows, and the
    // buckets are sized from the window. They are stored so the server plans
    // the same shape the builder runs.
    grain: hasChartShape(spec) ? autoGrain(spec.window) : "none",
    ungrouped: rows,
    dimensions: spec.dimensions.slice(0, MAX_DIMENSIONS),
    measures: measures.map((measure) => ({
      op: measure.op,
      field: measure.op === "count" ? "" : measure.field,
      alias: measureAlias(measure),
    })),
    filters: completeFilters(spec.filters),
    order_by:
      !rows && aliases.includes(spec.orderBy)
        ? [{ measure: spec.orderBy, direction: "desc" }]
        : [],
    limit: spec.limit,
  };
}

/**
 * The builder state a saved query restores, or null when its spec is not
 * one the builder can read. Whether the catalog can still answer it is a
 * separate question, asked of the result.
 */
export function specFromSavedSpec(
  dataset: string,
  saved: Record<string, unknown>,
): ExploreSpec | null {
  const {
    chart_type: chartType,
    window,
    dimensions,
    measures,
    filters,
    order_by: orderBy,
    limit,
  } = saved;
  if (!isChartType(chartType) || !isWindowPreset(window)) return null;
  if (!isStringArray(dimensions ?? [])) return null;
  const limitValue = limit ?? 0;
  if (
    typeof limitValue !== "number" ||
    !Number.isInteger(limitValue) ||
    limitValue < 0 ||
    limitValue > MAX_LIMIT
  ) {
    return null;
  }

  const measureDrafts: MeasureDraft[] = [];
  for (const measure of listOf(measures)) {
    if (!isRecord(measure) || typeof measure.op !== "string") return null;
    const field = measure.field ?? "";
    if (typeof field !== "string") return null;
    measureDrafts.push({ op: measure.op as MeasureOp, field });
  }

  const filterDrafts: FilterDraft[] = [];
  for (const filter of listOf(filters)) {
    if (!isRecord(filter)) return null;
    const { field, operator, values } = filter;
    if (typeof field !== "string" || !isFilterOperator(operator)) return null;
    if (!isStringArray(values ?? [])) return null;
    filterDrafts.push({ field, operator, values: (values ?? []) as string[] });
  }

  const order = listOf(orderBy)[0];
  const orderMeasure =
    isRecord(order) && typeof order.measure === "string" ? order.measure : "";

  return {
    dataset,
    measures: measureDrafts,
    filters: filterDrafts,
    dimensions: (dimensions ?? []) as string[],
    orderBy: orderMeasure,
    limit: limitValue,
    window,
    chartType,
  };
}

/**
 * The builder state as it would be saved, as comparable text: two specs that
 * save the same are the same query, however each was composed.
 */
export function savedSpecKey(spec: ExploreSpec): string {
  return JSON.stringify([spec.dataset, savedSpecFromSpec(spec)]);
}

/** Whether the builder holds anything the saved query does not. */
export function differsFromSaved(
  spec: ExploreSpec,
  query: { dataset: string; spec: Record<string, unknown> },
): boolean {
  const saved = specFromSavedSpec(query.dataset, query.spec);
  return saved === null || savedSpecKey(saved) !== savedSpecKey(spec);
}

function listOf(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isStringArray(value: unknown): value is string[] {
  return (
    Array.isArray(value) && value.every((item) => typeof item === "string")
  );
}
