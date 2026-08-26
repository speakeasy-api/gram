import type { ExploreDatasetSchema } from "@gram/client/models/components/exploredatasetschema.js";
import type { ExploreFieldMeta } from "@gram/client/models/components/explorefieldmeta.js";
import type { ExploreFilter } from "@gram/client/models/components/explorefilter.js";
import type { ExploreGroupExpression } from "@gram/client/models/components/exploregroupexpression.js";
import type { ExploreMetaResult } from "@gram/client/models/components/exploremetaresult.js";
import type { ExploreSavedQuery } from "@gram/client/models/components/exploresavedquery.js";
import type { QueryRequestBody } from "@gram/client/models/components/queryrequestbody.js";

export type ChartType = "line" | "bar" | "area" | "table" | "number";
export type WindowPreset = "1h" | "24h" | "7d" | "30d" | "90d";
export type DatasetName = ExploreDatasetSchema["name"];

export const CHART_TYPE_OPTIONS: { value: ChartType; label: string }[] = [
  { value: "line", label: "Line" },
  { value: "area", label: "Area" },
  { value: "bar", label: "Bar" },
  { value: "table", label: "Table" },
  { value: "number", label: "Number" },
];

export type FilterOp =
  | "in"
  | "not_in"
  | "contains"
  | "exists"
  | "eq"
  | "neq"
  | "gt"
  | "gte"
  | "lt"
  | "lte";

/** Display labels for the filter operators, keyed by wire name. */
export const FILTER_OP_LABELS: Record<FilterOp, string> = {
  in: "is any of",
  not_in: "is none of",
  contains: "contains",
  exists: "exists",
  eq: "=",
  neq: "!=",
  gt: ">",
  gte: ">=",
  lt: "<",
  lte: "<=",
};

function isFilterOp(op: string): op is FilterOp {
  return op in FILTER_OP_LABELS;
}

/** One filter row as edited in the builder. */
export interface FilterDraft {
  dimension: string;
  op?: FilterOp;
  values: string[];
}

/** One named conditional grouping row as edited in the builder. */
export interface GroupExpressionDraft {
  name: string;
  dimension: string;
  op?: FilterOp;
  values: string[];
}

/** The shape of a filter row's value control, decided by the operator. */
export type FilterValueControl = "multi" | "text" | "number" | "none";

export function valueControlForOp(op: FilterOp): FilterValueControl {
  switch (op) {
    case "in":
    case "not_in":
      return "multi";
    case "contains":
      return "text";
    case "exists":
      return "none";
    case "eq":
    case "neq":
    case "gt":
    case "gte":
    case "lt":
    case "lte":
      return "number";
  }
}

const WINDOW_SECONDS: Record<WindowPreset, number> = {
  "1h": 3_600,
  "24h": 86_400,
  "7d": 604_800,
  "30d": 2_592_000,
  "90d": 7_776_000,
};

export const WINDOW_OPTIONS: { value: WindowPreset; label: string }[] = [
  { value: "1h", label: "Last hour" },
  { value: "24h", label: "Last 24 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "90d", label: "Last 90 days" },
];

/** Whether the chart type renders a bucketed timeseries. */
export function isTimeseries(chartType: ChartType): boolean {
  return chartType === "line" || chartType === "bar" || chartType === "area";
}

/** Resolve a relative window into a stable, hour-aligned [from, to) range. */
export function windowRange(window: WindowPreset): { from: Date; to: Date } {
  const hourMs = 3_600_000;
  const to = new Date(Math.ceil(Date.now() / hourMs) * hourMs);
  const from = new Date(to.getTime() - WINDOW_SECONDS[window] * 1000);
  return { from, to };
}

/** Find a semantic dataset's schema. */
export function datasetMeta(
  meta: ExploreMetaResult | undefined,
  name: string,
): ExploreDatasetSchema | undefined {
  return meta?.datasets.find((dataset) => dataset.name === name);
}

/** Split a canonical calculation into its operation and target column. */
export function parseCanonical(canonical: string): {
  op: string;
  column: string;
} {
  const open = canonical.indexOf("(");
  if (open < 0 || !canonical.endsWith(")")) {
    return { op: canonical, column: "" };
  }
  return {
    op: canonical.slice(0, open),
    column: canonical.slice(open + 1, -1),
  };
}

/** Display label for a calculation, resolved through its dataset field. */
export function calculationDisplayLabel(
  meta: ExploreMetaResult | undefined,
  dataset: string,
  canonical: string,
): string {
  const { op, column } = parseCanonical(canonical);
  if (column === "") return op === "COUNT" ? "Count" : canonical;

  const schema = datasetMeta(meta, dataset);
  const label = fieldMeta(schema, column)?.label ?? column;
  return `${label} (${op.toLowerCase().replaceAll("_", " ")})`;
}

/** The unit a calculation carries, resolved through its dataset field. */
export function unitForCalculation(
  meta: ExploreMetaResult | undefined,
  dataset: string,
  canonical: string,
): string {
  const { op, column } = parseCanonical(canonical);
  if (op === "COUNT" || op === "COUNT_DISTINCT" || column === "") {
    return "count";
  }
  const schema = datasetMeta(meta, dataset);
  return fieldMeta(schema, column)?.unit || "count";
}

/** Distinct units carried by a query result's calculations. */
export function calculationUnits(
  meta: ExploreMetaResult | undefined,
  dataset: string,
  calculations: string[],
): string[] {
  return [
    ...new Set(
      calculations.map((calculation) =>
        unitForCalculation(meta, dataset, calculation),
      ),
    ),
  ];
}

/** Display label for a group-by field in one semantic dataset. */
export function dimensionDisplayLabel(
  meta: ExploreMetaResult | undefined,
  dataset: string,
  dimension: string,
): string {
  return fieldMeta(datasetMeta(meta, dataset), dimension)?.label ?? dimension;
}

/** The normalized operand list for an operator, or null while incomplete. */
function filterOperands(op: FilterOp, values: string[]): string[] | null {
  switch (op) {
    case "in":
    case "not_in": {
      const set = values.filter(
        (v, i, all) => v !== "" && all.indexOf(v) === i,
      );
      return set.length > 0 ? set : null;
    }
    case "exists":
      return [];
    case "contains":
    case "eq":
    case "neq":
    case "gt":
    case "gte":
    case "lt":
    case "lte": {
      // contains and the numeric comparisons read a single operand.
      const operand = values[0] ?? "";
      return operand.trim() === "" ? null : [operand];
    }
  }
}

/**
 * Drops incomplete filter rows, per operator: in/not_in need at least one
 * value, contains and the numeric comparisons a single non-empty operand,
 * exists none. The default "in" is left implicit on the wire.
 */
export function filtersFromDrafts(drafts: FilterDraft[]): ExploreFilter[] {
  const out: ExploreFilter[] = [];
  for (const d of drafts) {
    if (d.dimension === "") continue;
    const op = d.op ?? "in";
    const values = filterOperands(op, d.values);
    if (values === null) continue;
    out.push({
      dimension: d.dimension,
      op: op === "in" ? undefined : op,
      values,
    });
  }
  return out;
}

/** Drops incomplete conditional grouping rows and normalizes their operands. */
export function groupExpressionsFromDrafts(
  drafts: GroupExpressionDraft[],
): ExploreGroupExpression[] {
  const out: ExploreGroupExpression[] = [];
  for (const draft of drafts) {
    const name = draft.name.trim();
    if (name === "" || draft.dimension === "") continue;
    const op = draft.op ?? "in";
    const values = filterOperands(op, draft.values);
    if (values === null) continue;
    out.push({
      name,
      dimension: draft.dimension,
      op: op === "in" ? undefined : op,
      values,
    });
  }
  return out;
}

export type CalcOp =
  | "COUNT"
  | "COUNT_DISTINCT"
  | "SUM"
  | "AVG"
  | "MIN"
  | "MAX"
  | "P50"
  | "P95"
  | "P99";

export const CALC_OPS: CalcOp[] = [
  "COUNT",
  "COUNT_DISTINCT",
  "SUM",
  "AVG",
  "MIN",
  "MAX",
  "P50",
  "P95",
  "P99",
];

/** The dataset's grouping/filtering axes. */
export function dimensionFields(
  schema: ExploreDatasetSchema | undefined,
): ExploreFieldMeta[] {
  return (schema?.fields ?? []).filter((f) => f.role === "dimension");
}

/** The dataset's numeric quantities — the numeric calc ops' targets. */
export function measureFields(
  schema: ExploreDatasetSchema | undefined,
): ExploreFieldMeta[] {
  return (schema?.fields ?? []).filter((f) => f.role === "measure");
}

/** Find a field's schema entry. */
export function fieldMeta(
  schema: ExploreDatasetSchema | undefined,
  name: string,
): ExploreFieldMeta | undefined {
  return schema?.fields.find((f) => f.name === name);
}

/**
 * Fields a WHERE row can target: dimensions plus row-filterable measures —
 * anything the server declares at least one operator for.
 */
export function filterableFields(
  schema: ExploreDatasetSchema | undefined,
): ExploreFieldMeta[] {
  return (schema?.fields ?? []).filter((f) => f.filterOps.length > 0);
}

/** The operators legal on a field, in the server's declared order. */
export function opsForField(field: ExploreFieldMeta | undefined): FilterOp[] {
  return (field?.filterOps ?? []).filter(isFilterOp);
}

/** Keeps conditional groups whose source field and operator exist in a dataset. */
export function pruneGroupExpressionsForSchema(
  drafts: GroupExpressionDraft[],
  schema: ExploreDatasetSchema | undefined,
): GroupExpressionDraft[] {
  return drafts.filter((draft) => {
    const field = fieldMeta(schema, draft.dimension);
    return field !== undefined && opsForField(field).includes(draft.op ?? "in");
  });
}

/** Axiom-style type badge letter: S for strings, # for numeric fields. */
export function fieldBadgeLetter(type: string): string {
  return type === "string" ? "S" : "#";
}

/** One VISUALIZE row: an op and its target column ("" for COUNT). */
export interface CalcDraft {
  op: CalcOp;
  column: string;
}

/**
 * The canonical Explore state: a dataset-and-calculations query plus its
 * presentation and saved-query identity.
 */
export interface ExploreSpec {
  dataset: DatasetName;
  calculations: CalcDraft[];
  filters: FilterDraft[];
  groupBy: string[];
  groupExpressions: GroupExpressionDraft[];
  orderBy: string;
  limit: number;
  window: WindowPreset;
  chartType: ChartType;
  loadedQueryId: string | null;
  name: string;
}

/** Grain-appropriate calculations and breakdowns for a semantic dataset. */
export function defaultsForDataset(
  dataset: string,
): Pick<ExploreSpec, "calculations" | "groupBy"> {
  switch (dataset) {
    case "events":
      return {
        calculations: [{ op: "COUNT", column: "" }],
        groupBy: ["event_name"],
      };
    case "turn_usage":
      return {
        calculations: [
          { op: "SUM", column: "input_tokens" },
          { op: "SUM", column: "output_tokens" },
        ],
        groupBy: ["response_model"],
      };
    case "user_usage":
      return {
        calculations: [{ op: "SUM", column: "cost_usd" }],
        groupBy: ["user_key"],
      };
    default:
      return {
        calculations: [{ op: "COUNT", column: "" }],
        groupBy: [],
      };
  }
}

const DEFAULT_DATASET_SPEC = defaultsForDataset("events");

export const DEFAULT_SPEC: ExploreSpec = {
  dataset: "events",
  calculations: DEFAULT_DATASET_SPEC.calculations,
  filters: [],
  groupBy: DEFAULT_DATASET_SPEC.groupBy,
  groupExpressions: [],
  orderBy: "",
  limit: 0,
  window: "7d",
  chartType: "line",
  loadedQueryId: null,
  name: "",
};

/** The calculation's canonical name: COUNT, or OP(column). */
export function canonicalCalc(c: CalcDraft): string {
  if (c.column === "") return c.op;
  return `${c.op}(${c.column})`;
}

/** Drops incomplete calculation rows (no column picked yet). */
export function completeCalcs(drafts: CalcDraft[]): CalcDraft[] {
  return drafts.filter((c) => c.op === "COUNT" || c.column !== "");
}

/**
 * Honeycomb-style automatic bucket width: pick a granularity that yields a
 * readable series for the window (floored at the engine's 60s minimum).
 */
export function autoGranularity(window: WindowPreset): number {
  switch (window) {
    case "1h":
      return 60;
    case "24h":
      return 600;
    case "7d":
      return 3600;
    case "30d":
      return 14400;
    case "90d":
      return 43200;
  }
}

/**
 * Build the query request body for a timeseries or whole-range summary.
 */
export function queryBodyFromSpec(
  spec: ExploreSpec,
  shape: "timeseries" | "summary",
): QueryRequestBody {
  const { from, to } = windowRange(spec.window);
  const calculations = completeCalcs(spec.calculations);
  const orderValid = calculations.some(
    (c) => canonicalCalc(c) === spec.orderBy,
  );
  return {
    from,
    to,
    dataset: spec.dataset,
    calculations: calculations.map((c) => ({
      op: c.op,
      column: c.column === "" ? undefined : c.column,
    })),
    groupBy: spec.chartType === "number" ? [] : spec.groupBy,
    groupExpressions:
      spec.chartType === "number"
        ? []
        : groupExpressionsFromDrafts(spec.groupExpressions),
    filters: filtersFromDrafts(spec.filters),
    granularitySeconds:
      shape === "timeseries" ? autoGranularity(spec.window) : 0,
    sortBy: shape === "summary" && orderValid ? spec.orderBy : undefined,
    sortDesc: true,
    limit: shape === "summary" ? spec.limit : 0,
  };
}

/** Load a saved calculation query back into the canonical builder. */
export function specFromSavedQuery(q: ExploreSavedQuery): ExploreSpec {
  return {
    dataset: q.dataset,
    calculations: q.calculations.map((calculation) => ({
      op: calculation.op,
      column: calculation.column ?? "",
    })),
    filters: q.filters.map((filter) => ({
      dimension: filter.dimension,
      op: filter.op,
      values: [...filter.values],
    })),
    groupBy: [...q.groupBy],
    groupExpressions: q.groupExpressions.map((expression) => ({
      name: expression.name,
      dimension: expression.dimension,
      op: expression.op,
      values: [...expression.values],
    })),
    orderBy: q.sortBy,
    limit: q.limit,
    window: q.window,
    chartType: q.chartType,
    loadedQueryId: q.id,
    name: q.name,
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

/** Format an aggregated value for display, by its unit. */
export function formatMeasureValue(value: number, unit: string): string {
  switch (unit) {
    case "usd":
      return usdFormatter.format(value);
    case "ms":
      return `${Math.round(value).toLocaleString()} ms`;
    case "ratio":
      return percentFormatter.format(value);
    default:
      return compactFormatter.format(value);
  }
}
