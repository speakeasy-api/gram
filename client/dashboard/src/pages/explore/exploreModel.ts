import type { AnalyticsDataset } from "@gram/client/models/components/analyticsdataset.js";
import type { AnalyticsField } from "@gram/client/models/components/analyticsfield.js";
import type { AnalyticsFilter } from "@gram/client/models/components/analyticsfilter.js";
import type { AnalyticsMeasure } from "@gram/client/models/components/analyticsmeasure.js";
import type { AnalyticsQueryPayload } from "@gram/client/models/components/analyticsquerypayload.js";

// The Explore state core. Everything the builder offers is read off the
// catalog that `analytics.describe` returns: which datasets exist, which
// fields each has, what each field can be filtered or aggregated by. Nothing
// here knows a dataset by name, so a new dataset ships with no client change.

export type ChartType = "line" | "area" | "bar" | "table" | "number";
export type WindowPreset = "1h" | "24h" | "7d" | "30d" | "90d";
export type Grain = NonNullable<AnalyticsQueryPayload["grain"]>;
export type MeasureOp = AnalyticsMeasure["op"];
export type FilterOperator = AnalyticsFilter["operator"];

// Mirrors of the server's guardrails (server/internal/telemetry/analytics/
// catalog.go). The server is authoritative; these only keep the builder from
// offering something it would reject.
export const MAX_DIMENSIONS = 3;
export const DEFAULT_LIMIT = 100;
export const MAX_LIMIT = 1000;

export const CHART_TYPE_OPTIONS: { value: ChartType; label: string }[] = [
  { value: "line", label: "Line" },
  { value: "area", label: "Area" },
  { value: "bar", label: "Bar" },
  { value: "table", label: "Table" },
  { value: "number", label: "Number" },
];

export const WINDOW_OPTIONS: { value: WindowPreset; label: string }[] = [
  { value: "1h", label: "Last hour" },
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "90d", label: "Last 90 days" },
];

const WINDOW_SECONDS: Record<WindowPreset, number> = {
  "1h": 3_600,
  "24h": 86_400,
  "7d": 604_800,
  "30d": 2_592_000,
  "90d": 7_776_000,
};

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
  /** Alias of the measure the summary sorts by; "" keeps the group order. */
  orderBy: string;
  /** Row cap for the summary; 0 defers to the server default. */
  limit: number;
  window: WindowPreset;
  chartType: ChartType;
}

/** Resolve a relative window into a stable, hour-aligned [from, to) range. */
export function windowRange(
  window: WindowPreset,
  now: number = Date.now(),
): { from: Date; to: Date } {
  const hourMs = 3_600_000;
  const to = new Date(Math.ceil(now / hourMs) * hourMs);
  const from = new Date(to.getTime() - WINDOW_SECONDS[window] * 1000);
  return { from, to };
}

/**
 * The bucket width for a window: the finest grain the catalog offers that
 * still yields a readable series. The catalog's finest grain is an hour.
 */
export function autoGrain(window: WindowPreset): Grain {
  switch (window) {
    case "1h":
    case "24h":
      return "hour";
    case "7d":
    case "30d":
      return "day";
    case "90d":
      return "week";
  }
}

/** Whether the chart type renders a bucketed timeseries. */
export function isTimeseries(chartType: ChartType): boolean {
  return chartType === "line" || chartType === "area" || chartType === "bar";
}

/** The grain a spec queries at: buckets for a timeseries, none otherwise. */
export function grainForSpec(spec: ExploreSpec): Grain {
  return isTimeseries(spec.chartType) ? autoGrain(spec.window) : "none";
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
export function measureFields(
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

function isFilterOperator(operator: string): operator is FilterOperator {
  return operator in FILTER_OPERATOR_LABELS;
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
export function completeFilters(drafts: FilterDraft[]): AnalyticsFilter[] {
  const out: AnalyticsFilter[] = [];
  for (const draft of drafts) {
    if (draft.field === "") continue;
    const values = draft.values
      .map((value) => value.trim())
      .filter(
        (value, index, all) => value !== "" && all.indexOf(value) === index,
      );
    if (values.length === 0) continue;
    out.push({
      field: draft.field,
      operator: draft.operator,
      values: draft.operator === "equals" ? values.slice(0, 1) : values,
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

/** A fresh spec for a dataset, keeping the window and limit already chosen. */
export function specForDataset(
  dataset: AnalyticsDataset,
  current?: Pick<ExploreSpec, "window" | "limit">,
): ExploreSpec {
  const summary = fieldByName(dataset, dataset.summaryField ?? "");
  return {
    dataset: dataset.name,
    measures: [{ op: "count", field: "" }],
    filters: [],
    dimensions: summary && summary.role === "dimension" ? [summary.name] : [],
    orderBy: "",
    limit: current?.limit ?? 0,
    window: current?.window ?? "7d",
    chartType: defaultChartForKind(dataset.kind),
  };
}

/** The spec the page opens on: the catalog's first dataset, or nothing. */
export function initialSpec(datasets: AnalyticsDataset[]): ExploreSpec | null {
  const first = datasets[0];
  return first ? specForDataset(first) : null;
}

/** Parse the LIMIT control's text into a spec limit (0 = server default). */
export function parseLimit(raw: string): number {
  const parsed = Number(raw);
  if (!Number.isInteger(parsed) || parsed <= 0) return 0;
  return Math.min(parsed, MAX_LIMIT);
}

/** Build the query the spec describes. */
export function queryBodyFromSpec(spec: ExploreSpec): AnalyticsQueryPayload {
  const { from, to } = windowRange(spec.window);
  const measures = completeMeasures(spec.measures);
  const aliases = measures.map(measureAlias);
  const grouped = spec.chartType !== "number";
  return {
    dataset: spec.dataset,
    from,
    to,
    grain: grainForSpec(spec),
    dimensions: grouped ? spec.dimensions.slice(0, MAX_DIMENSIONS) : [],
    measures: measures.map((measure) => ({
      op: measure.op,
      field: measure.op === "count" ? undefined : measure.field,
      alias: measureAlias(measure),
    })),
    filters: completeFilters(spec.filters),
    orderBy: aliases.includes(spec.orderBy)
      ? [{ measure: spec.orderBy, direction: "desc" }]
      : undefined,
    limit: spec.limit > 0 ? spec.limit : undefined,
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
      return `${Math.round(value).toLocaleString()} ms`;
    case "s":
      return `${compactFormatter.format(value)} s`;
    case "ratio":
      return percentFormatter.format(value);
    default:
      return compactFormatter.format(value);
  }
}
