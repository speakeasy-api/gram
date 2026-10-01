import {
  autoGrain,
  completeFilters,
  completeMeasures,
  hasChartShape,
  isChartType,
  isFilterOperator,
  isMeasureOp,
  isRowsMode,
  isWindowPreset,
  MAX_LIMIT,
  measureAlias,
  queryDimensions,
  type ChartType,
  type ExploreSpec,
  type FilterDraft,
  type MeasureDraft,
} from "./exploreModel";

// A widget keeps the builder state split the way the widgets service reads
// it (server/internal/widgets/validate.go): the query is the question in
// catalog vocabulary — snake_case, measures with their aliases, order as a
// list — plus its window; the visualization is the chart that draws it. Rows
// still being composed are not part of the question, so they are not saved.

/** What a widget stores for the builder's state. */
export interface WidgetState {
  query: Record<string, unknown>;
  visualization: Record<string, unknown>;
}

/**
 * The chart a spec is drawn with, as the server will check it. Rows draw only
 * as a table and a number tile is never broken down, whatever the builder's
 * controls were last left on, so that is what is saved.
 */
function drawnChart(spec: ExploreSpec): ChartType {
  return isRowsMode(spec) ? "table" : spec.chartType;
}

/** The query and visualization a widget stores for the builder's state. */
export function widgetFromSpec(spec: ExploreSpec): WidgetState {
  const measures = completeMeasures(spec.measures);
  const rows = measures.length === 0;
  const aliases = measures.map(measureAlias);
  const chart = drawnChart(spec);
  const timeseries = hasChartShape(spec);
  return {
    query: {
      window: spec.window,
      // The widget stores the one query the builder runs: its mode and grain
      // (no measure means rows, and buckets are sized from the window), and
      // for a timeseries the server's row cap in time order, since order and
      // limit only shape whole-window charts.
      grain: timeseries ? autoGrain(spec.window) : "none",
      ungrouped: rows,
      // The dimensions the builder's query asked for, so the saved widget
      // shows the columns that were on screen.
      dimensions: queryDimensions(spec),
      measures: measures.map((measure) => ({
        op: measure.op,
        field: measure.op === "count" ? "" : measure.field,
        alias: measureAlias(measure),
      })),
      filters: completeFilters(spec.filters),
      order_by:
        !rows && !timeseries && aliases.includes(spec.orderBy)
          ? [{ measure: spec.orderBy, direction: "desc" }]
          : [],
      limit: timeseries ? MAX_LIMIT : spec.limit,
    },
    visualization: { type: chart, options: {} },
  };
}

/**
 * The builder state a widget restores, or null when it is not one the
 * builder can read. Whether the catalog can still answer it is a separate
 * question, asked of the result.
 */
export function specFromWidget(
  dataset: string,
  query: Record<string, unknown>,
  visualization: Record<string, unknown>,
): ExploreSpec | null {
  const chartType = visualization.type;
  const {
    window,
    dimensions,
    measures,
    filters,
    order_by: orderBy,
    limit,
  } = query;
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
    if (!isRecord(measure) || !isMeasureOp(measure.op)) return null;
    const field = measure.field ?? "";
    if (typeof field !== "string") return null;
    measureDrafts.push({ op: measure.op, field });
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
  // The builder only ranks descending, so a widget ordered otherwise is not
  // one it can open without changing the question.
  if (isRecord(order) && (order.direction ?? "desc") !== "desc") return null;

  const spec: ExploreSpec = {
    dataset,
    measures: measureDrafts,
    filters: filterDrafts,
    dimensions: (dimensions ?? []) as string[],
    orderBy: orderMeasure,
    limit: limitValue,
    window,
    chartType,
  };
  // Nor can it keep a grain other than the one it sizes from the window.
  const builderGrain = widgetFromSpec(spec).query.grain;
  if ((query.grain ?? builderGrain) !== builderGrain) return null;
  // A timeseries runs in time order at the server's cap, so the builder's
  // order and limit stay unset for it; a timeseries widget stored with any
  // other order or limit is not one the builder ran.
  if (hasChartShape(spec)) {
    if (orderMeasure !== "" || limitValue !== MAX_LIMIT) return null;
    return { ...spec, orderBy: "", limit: 0 };
  }
  return spec;
}

/** A stored widget, as far as restoring it into the builder goes. */
export interface StoredWidget {
  dataset: string;
  query: Record<string, unknown>;
  visualization: Record<string, unknown>;
}

/** The builder state a stored widget restores, or null. */
export function specFromStoredWidget(widget: StoredWidget): ExploreSpec | null {
  return specFromWidget(widget.dataset, widget.query, widget.visualization);
}

/**
 * The builder state as it would be saved, as comparable text: two specs that
 * save the same are the same widget, however each was composed.
 */
export function widgetKey(spec: ExploreSpec): string {
  return JSON.stringify([spec.dataset, widgetFromSpec(spec)]);
}

/** Whether the builder holds anything the widget does not. */
export function differsFromWidget(
  spec: ExploreSpec,
  widget: StoredWidget,
): boolean {
  const saved = specFromStoredWidget(widget);
  return saved === null || widgetKey(saved) !== widgetKey(spec);
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
