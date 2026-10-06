import type { DateRangePreset } from "@/elements";
import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { AnalyticsField } from "@gram/client/models/components/analyticsfield.js";
import type { AnalyticsFilter } from "@gram/client/models/components/analyticsfilter.js";
import type { AnalyticsMeasure } from "@gram/client/models/components/analyticsmeasure.js";
import type { AnalyticsQueryPayload } from "@gram/client/models/components/analyticsquerypayload.js";
import type { AnalyticsQueryResult } from "@gram/client/models/components/analyticsqueryresult.js";

// The Explore state core. Everything the builder offers is read off the
// catalog that `analytics.describe` returns: which datasets exist, which
// fields each has, what each field can be filtered or aggregated by. Nothing
// here knows a dataset by name, so a new dataset ships with no client change.

export type ChartType = "line" | "area" | "bar" | "ranked" | "table" | "number";
/**
 * A relative window: the dashboard's date-range presets, so a widget, the
 * builder and the page around them speak one vocabulary.
 */
export type WindowPreset = DateRangePreset;
export type Grain = NonNullable<AnalyticsQueryPayload["grain"]>;
export type MeasureOp = AnalyticsMeasure["op"];
export type FilterOperator = AnalyticsFilter["operator"];
export type ResultRow = AnalyticsQueryResult["rows"][number];

// Mirrors of the server's guardrails (server/internal/telemetry/analytics/
// catalog.go). The server is authoritative; these only keep the builder from
// offering something it would reject.
export const MAX_DIMENSIONS = 3;
export const DEFAULT_LIMIT = 100;
export const MAX_LIMIT = 1000;

// Result columns the server adds beside the requested ones: the bucket start
// of a grouped, bucketed query and the event time of a row.
export const TIME_BUCKET_COLUMN = "time_bucket";
export const TIME_COLUMN = "time";

export const CHART_TYPE_OPTIONS: { value: ChartType; label: string }[] = [
  { value: "line", label: "Line" },
  { value: "area", label: "Area" },
  { value: "bar", label: "Bar" },
  { value: "ranked", label: "Ranked" },
  { value: "table", label: "Table" },
  { value: "number", label: "Number" },
];

/** The windows the builder offers, shortest first: every dashboard preset. */
export const WINDOW_PRESETS: readonly WindowPreset[] = [
  "15m",
  "1h",
  "4h",
  "1d",
  "2d",
  "3d",
  "7d",
  "15d",
  "30d",
  "90d",
];

// Queries run as you build, and the dataset is scanned across the whole
// window however selective the filters are, so the window opens short and
// widening it is the moment someone chooses to pay for more.
const DEFAULT_WINDOW: WindowPreset = "1d";

const WINDOW_SECONDS: Record<WindowPreset, number> = {
  "15m": 900,
  "1h": 3_600,
  "4h": 14_400,
  "1d": 86_400,
  "2d": 172_800,
  "3d": 259_200,
  "7d": 604_800,
  "15d": 1_296_000,
  "30d": 2_592_000,
  "90d": 7_776_000,
};

// The builder's first spelling of a day, before it took the dashboard's
// presets. Widgets and links saved with it still open, as "1d".
const LEGACY_WINDOWS: Record<string, WindowPreset> = { "24h": "1d" };

/**
 * A stored or linked window in today's vocabulary, or null when it is not
 * one: a preset as it is, or an older spelling of one.
 */
export function windowPreset(value: unknown): WindowPreset | null {
  if (isWindowPreset(value)) return value;
  if (typeof value === "string" && Object.hasOwn(LEGACY_WINDOWS, value)) {
    return LEGACY_WINDOWS[value]!;
  }
  return null;
}

/** One VISUALIZE row: an aggregation and its target field ("" for count). */
export interface MeasureDraft {
  op: MeasureOp;
  field: string;
}

/** One WHERE row as edited in the builder. */
export interface FilterDraft {
  field: string;
  operator: FilterOperator;
  values: string[];
}

/** The canonical Explore state: one query plus how to present it. */
export interface ExploreSpec {
  dataset: string;
  measures: MeasureDraft[];
  filters: FilterDraft[];
  dimensions: string[];
  /**
   * Alias of the measure a whole-window result sorts by; "" keeps the group
   * order. A timeseries chart is in time order and ignores it.
   */
  orderBy: string;
  /**
   * Row cap for a whole-window result; 0 defers to the server default. A
   * timeseries chart is capped at MAX_LIMIT instead.
   */
  limit: number;
  window: WindowPreset;
  /**
   * An absolute range that replaces the window, set when a page asks a
   * widget over its own time range or a chart is dragged across. A widget
   * keeps a relative window, so a spec with a range cannot be saved as one.
   */
  range?: TimeRange | undefined;
  chartType: ChartType;
}

/**
 * An absolute [from, to) range, in Unix milliseconds, with the label the
 * date picker gave it ("Last Tuesday"), if any.
 */
export interface TimeRange {
  from: number;
  to: number;
  label?: string | undefined;
}

/**
 * Resolve a relative window into a stable [from, to) range. It ends on the
 * next hour, so a query's cache key holds within the hour; a window shorter
 * than an hour ends on the next minute instead, or most of it would lie in
 * the future.
 */
export function windowRange(
  window: WindowPreset,
  now: number = Date.now(),
): { from: Date; to: Date } {
  const span = WINDOW_SECONDS[window] * 1000;
  const step = span < 3_600_000 ? 60_000 : 3_600_000;
  const to = new Date(Math.ceil(now / step) * step);
  const from = new Date(to.getTime() - span);
  return { from, to };
}

/**
 * The bucket width for a window: the finest grain the catalog offers that
 * still yields a readable series. The catalog's finest grain is an hour.
 */
export function autoGrain(window: WindowPreset): Grain {
  return grainForSpan(WINDOW_SECONDS[window] * 1000);
}

/**
 * The bucket width for a span of time: hours up to three days, days up to
 * thirty, weeks past that.
 */
function grainForSpan(ms: number): Grain {
  if (ms <= WINDOW_SECONDS["3d"] * 1000) return "hour";
  if (ms <= WINDOW_SECONDS["30d"] * 1000) return "day";
  return "week";
}

/** The [from, to) a spec asks over: its range, or its window resolved. */
export function specRange(
  spec: Pick<ExploreSpec, "window" | "range">,
  now: number = Date.now(),
): { from: Date; to: Date } {
  if (spec.range) {
    return { from: new Date(spec.range.from), to: new Date(spec.range.to) };
  }
  return windowRange(spec.window, now);
}

/** The bucket width a spec's timeseries is drawn at. */
export function specGrain(spec: Pick<ExploreSpec, "window" | "range">): Grain {
  return spec.range
    ? grainForSpan(spec.range.to - spec.range.from)
    : autoGrain(spec.window);
}

/** Whether the chart type renders a bucketed timeseries. */
function isTimeseries(chartType: ChartType): boolean {
  return chartType === "line" || chartType === "area" || chartType === "bar";
}

export function findDataset(
  datasets: AnalyticsDataset[],
  name: string,
): AnalyticsDataset | undefined {
  return datasets.find((dataset) => dataset.name === name);
}

export function fieldByName(
  dataset: AnalyticsDataset | undefined,
  name: string,
): AnalyticsField | undefined {
  return dataset?.fields.find((field) => field.name === name);
}

/** The dataset's grouping axes. */
export function dimensionFields(
  dataset: AnalyticsDataset | undefined,
): AnalyticsField[] {
  return (dataset?.fields ?? []).filter((field) => field.role === "dimension");
}

/** The dataset's numeric quantities. */
function measureFields(
  dataset: AnalyticsDataset | undefined,
): AnalyticsField[] {
  return (dataset?.fields ?? []).filter((field) => field.role === "measure");
}

/** Fields a WHERE row can target: anything the catalog declares operators for. */
export function filterableFields(
  dataset: AnalyticsDataset | undefined,
): AnalyticsField[] {
  return (dataset?.fields ?? []).filter(
    (field) => (field.operators ?? []).length > 0,
  );
}

// The order aggregations are offered in, whichever fields declare them.
const MEASURE_OP_ORDER: MeasureOp[] = [
  "count",
  "sum",
  "avg",
  "min",
  "max",
  "p50",
  "p95",
  "p99",
];

export function isMeasureOp(value: unknown): value is MeasureOp {
  return MEASURE_OP_ORDER.some((op) => op === value);
}

/**
 * The aggregations a dataset admits: count (every dataset), then every op
 * at least one measure field declares.
 */
export function opsForDataset(
  dataset: AnalyticsDataset | undefined,
): MeasureOp[] {
  const declared = new Set<string>();
  for (const field of measureFields(dataset)) {
    for (const aggregation of field.aggregations ?? []) {
      declared.add(aggregation);
    }
  }
  return MEASURE_OP_ORDER.filter((op) => op === "count" || declared.has(op));
}

/** The measure fields an aggregation can target; count targets none. */
export function fieldsForOp(
  dataset: AnalyticsDataset | undefined,
  op: MeasureOp,
): AnalyticsField[] {
  if (op === "count") return [];
  return measureFields(dataset).filter((field) =>
    (field.aggregations ?? []).includes(op),
  );
}

export const FILTER_OPERATOR_LABELS: Record<FilterOperator, string> = {
  equals: "is",
  in: "is any of",
};

export function isFilterOperator(value: unknown): value is FilterOperator {
  return (
    typeof value === "string" && Object.hasOwn(FILTER_OPERATOR_LABELS, value)
  );
}

function isWindowPreset(value: unknown): value is WindowPreset {
  return WINDOW_PRESETS.some((preset) => preset === value);
}

export function isChartType(value: unknown): value is ChartType {
  return CHART_TYPE_OPTIONS.some((option) => option.value === value);
}

/**
 * A fresh filter on a field: its first admitted operator and no values, so
 * values picked for one dimension never carry over to another.
 */
export function filterForField(
  dataset: AnalyticsDataset | undefined,
  name: string,
): FilterDraft {
  const operator = operatorsForField(fieldByName(dataset, name))[0] ?? "in";
  return { field: name, operator, values: [] };
}

/** The operators legal on a field, in the catalog's declared order. */
export function operatorsForField(
  field: AnalyticsField | undefined,
): FilterOperator[] {
  return (field?.operators ?? []).filter(isFilterOperator);
}

/** The result column a measure lands in: count, or op_field. */
export function measureAlias(measure: MeasureDraft): string {
  if (measure.op === "count") return "count";
  return `${measure.op}_${measure.field}`;
}

/** How a measure reads in the builder and the results: COUNT, or OP(field). */
export function measureLabel(measure: MeasureDraft): string {
  if (measure.op === "count") return "COUNT";
  return `${measure.op.toUpperCase()}(${measure.field})`;
}

/** The unit a measure's values carry: its field's, or none for a count. */
export function measureUnit(
  dataset: AnalyticsDataset | undefined,
  measure: MeasureDraft,
): string {
  if (measure.op === "count") return "";
  return fieldByName(dataset, measure.field)?.unit ?? "";
}

/** Drops measure rows still waiting on a field, and duplicates. */
export function completeMeasures(drafts: MeasureDraft[]): MeasureDraft[] {
  const seen = new Set<string>();
  const out: MeasureDraft[] = [];
  for (const draft of drafts) {
    if (draft.op !== "count" && draft.field === "") continue;
    const alias = measureAlias(draft);
    if (seen.has(alias)) continue;
    seen.add(alias);
    out.push(draft);
  }
  return out;
}

/**
 * Drops incomplete filter rows: no field, or no value. `equals` reads one
 * operand; `in` reads every distinct non-empty one.
 */
/** At most this many values per filter, matching the analytics design. */
export const MAX_FILTER_VALUES = 100;

/** A filter's values as the query sends them: trimmed, non-blank, once each. */
function distinctValues(values: string[]): string[] {
  return values
    .map((value) => value.trim())
    .filter(
      (value, index, all) => value !== "" && all.indexOf(value) === index,
    );
}

export function completeFilters(drafts: FilterDraft[]): AnalyticsFilter[] {
  // The server takes at most this many values per filter and rejects the
  // whole query past it, so the builder never asks for more.
  const out: AnalyticsFilter[] = [];
  for (const draft of drafts) {
    if (draft.field === "") continue;
    const values = distinctValues(draft.values);
    if (values.length === 0) continue;
    out.push({
      field: draft.field,
      operator: draft.operator,
      values:
        draft.operator === "equals"
          ? values.slice(0, 1)
          : values.slice(0, MAX_FILTER_VALUES),
    });
  }
  return out;
}

// `kind` selects only the hardcoded bits: the chart a dataset opens on.
function defaultChartForKind(kind: AnalyticsDataset["kind"]): ChartType {
  switch (kind) {
    case "event":
      return "line";
    case "metric":
      return "bar";
  }
}

/**
 * The dimensions a dataset opens grouped by: the ones the catalog flags as
 * default, in declaration order, within the builder's own cap.
 */
function defaultDimensions(dataset: AnalyticsDataset): string[] {
  return dataset.fields
    .filter((f) => f.default && f.role === "dimension")
    .map((f) => f.name)
    .slice(0, MAX_DIMENSIONS);
}

/** A fresh spec for a dataset, keeping the window and limit already chosen. */
export function specForDataset(
  dataset: AnalyticsDataset,
  current?: Pick<ExploreSpec, "window" | "limit">,
): ExploreSpec {
  return {
    dataset: dataset.name,
    measures: [{ op: "count", field: "" }],
    filters: [],
    dimensions: defaultDimensions(dataset),
    orderBy: "",
    limit: current?.limit ?? 0,
    window: current?.window ?? DEFAULT_WINDOW,
    chartType: defaultChartForKind(dataset.kind),
  };
}

/** The spec the page opens on: the catalog's first dataset, or nothing. */
export function initialSpec(datasets: AnalyticsDataset[]): ExploreSpec | null {
  const first = datasets[0];
  return first ? specForDataset(first) : null;
}

/**
 * What a spec asks for that the catalog no longer offers, naming the first
 * missing piece, or "" when every part of it still resolves. Rows still
 * being composed — a measure waiting on its field, a filter with no field
 * yet — are builder state, not breakage, so they pass.
 */
export function specProblem(
  datasets: AnalyticsDataset[],
  spec: ExploreSpec,
): string {
  const dataset = findDataset(datasets, spec.dataset);
  if (!dataset) return `dataset "${spec.dataset}" does not exist`;

  const ops = opsForDataset(dataset);
  for (const measure of spec.measures) {
    if (!ops.includes(measure.op)) {
      return `${spec.dataset} has no ${measure.op} aggregation`;
    }
    // Count takes no field, and a query run would drop one silently.
    if (measure.op === "count") {
      if (measure.field !== "") return "count takes no field";
      continue;
    }
    if (measure.field === "") continue;
    const fields = fieldsForOp(dataset, measure.op).map((field) => field.name);
    if (!fields.includes(measure.field)) {
      return `field "${measure.field}" cannot be aggregated by ${measure.op} in ${spec.dataset}`;
    }
  }

  // The builder never asks for more dimensions than the cap or one twice,
  // so a spec that does was not made by it.
  if (
    spec.dimensions.length > MAX_DIMENSIONS ||
    new Set(spec.dimensions).size !== spec.dimensions.length
  ) {
    return `query asks for duplicate or more than ${MAX_DIMENSIONS} dimensions`;
  }
  const dimensions = dimensionFields(dataset).map((field) => field.name);
  for (const dimension of spec.dimensions) {
    if (!dimensions.includes(dimension)) {
      return `field "${dimension}" is not a dimension of ${spec.dataset}`;
    }
  }

  for (const filter of spec.filters) {
    if (filter.field === "") continue;
    // Past these bounds the query run would silently drop values, so the
    // spec is refused rather than answered as a different question.
    const values = distinctValues(filter.values);
    if (
      values.length > MAX_FILTER_VALUES ||
      (filter.operator === "equals" && values.length > 1)
    ) {
      return `filter "${filter.field}" has too many values`;
    }
    const field = fieldByName(dataset, filter.field);
    if (!operatorsForField(field).includes(filter.operator)) {
      return `field "${filter.field}" cannot be filtered by ${filter.operator} in ${spec.dataset}`;
    }
  }

  if (
    spec.orderBy !== "" &&
    !completeMeasures(spec.measures).map(measureAlias).includes(spec.orderBy)
  ) {
    return `order by "${spec.orderBy}" names no measure in the query`;
  }
  return "";
}

/** Parse the LIMIT control's text into a spec limit (0 = server default). */
export function parseLimit(raw: string): number {
  const parsed = Number(raw);
  if (!Number.isInteger(parsed) || parsed <= 0) return 0;
  return Math.min(parsed, MAX_LIMIT);
}

/**
 * Whether the spec asks for rows at the dataset's grain rather than an
 * aggregate: a dataset with nothing to measure means rows.
 */
export function isRowsMode(spec: ExploreSpec): boolean {
  return completeMeasures(spec.measures).length === 0;
}

/** The dimensions a query groups or projects by; a number tile has none. */
export function queryDimensions(spec: ExploreSpec): string[] {
  if (spec.chartType === "number") return [];
  return spec.dimensions.slice(0, MAX_DIMENSIONS);
}

/** Whether the spec draws a bucketed timeseries over at least one measure. */
export function hasChartShape(spec: ExploreSpec): boolean {
  return isTimeseries(spec.chartType) && !isRowsMode(spec);
}

/**
 * Build the one query the spec runs. A timeseries chart asks for bucketed
 * rows; every other chart asks for whole-window figures, which the order and
 * limit apply to. Someone who wants the figures behind a timeseries switches
 * the chart to a table.
 */
export function queryBodyFromSpec(spec: ExploreSpec): AnalyticsQueryPayload {
  const { from, to } = specRange(spec);
  const measures = completeMeasures(spec.measures);
  const dimensions = queryDimensions(spec);
  const filters = completeFilters(spec.filters);
  const limit = spec.limit > 0 ? spec.limit : undefined;

  if (measures.length === 0) {
    // Rows at the dataset's grain, newest first; the dimensions are the
    // projection rather than a grouping.
    return {
      dataset: spec.dataset,
      from,
      to,
      grain: "none",
      dimensions,
      filters,
      ungrouped: true,
      limit,
    };
  }

  const base = {
    dataset: spec.dataset,
    from,
    to,
    dimensions,
    measures: measures.map((measure) => ({
      op: measure.op,
      field: measure.op === "count" ? undefined : measure.field,
      alias: measureAlias(measure),
    })),
    filters,
  };
  if (hasChartShape(spec)) {
    // Buckets multiply rows: a day of hourly buckets over ten users is 240
    // of them, so the cap is the server's maximum rather than the builder's
    // limit. Bucketed rows come back in time order, so no order is sent.
    return { ...base, grain: specGrain(spec), limit: MAX_LIMIT };
  }
  const aliases = measures.map(measureAlias);
  return {
    ...base,
    grain: "none",
    orderBy: aliases.includes(spec.orderBy)
      ? [{ measure: spec.orderBy, direction: "desc" }]
      : undefined,
    limit,
  };
}

const usdFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  maximumFractionDigits: 2,
});

const compactFormatter = new Intl.NumberFormat("en", {
  notation: "compact",
  maximumFractionDigits: 1,
});

// Pinned like the others: a bare toLocaleString would follow the host, so
// the same number read differently on a non-US machine.
const integerFormatter = new Intl.NumberFormat("en", {
  maximumFractionDigits: 0,
});

const percentFormatter = new Intl.NumberFormat("en", {
  style: "percent",
  maximumFractionDigits: 1,
});

/** Format an aggregated value for display, by the field's unit. */
export function formatMeasureValue(value: number, unit: string): string {
  switch (unit) {
    case "usd":
      return usdFormatter.format(value);
    case "ms":
      return `${integerFormatter.format(Math.round(value))} ms`;
    case "s":
      return `${compactFormatter.format(value)} s`;
    case "ratio":
      return percentFormatter.format(value);
    default:
      return compactFormatter.format(value);
  }
}

/** A result cell as a number, or null when it is not one. */
export function numericCell(value: unknown): number | null {
  if (typeof value === "number") return Number.isFinite(value) ? value : null;
  if (typeof value === "bigint") return Number(value);
  if (typeof value === "string" && value.trim() !== "") {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : null;
  }
  return null;
}

/** A result cell as text: the value as the producer stated it, or a dash. */
export function textCell(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "bigint") {
    return String(value);
  }
  if (typeof value === "boolean") return value ? "true" : "false";
  return JSON.stringify(value);
}
