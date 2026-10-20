import { defineFilters } from "@/components/filters";
import type { AnalyticsQueryPayload } from "@gram/client/models/components/analyticsquerypayload.js";
import type { AnalyticsSignalCatalog } from "@gram/client/models/components/analyticssignalcatalog.js";
import type { AnalyticsQueryResult } from "@gram/client/models/components/analyticsqueryresult.js";
import type { SeriesSet } from "@/pages/explore/resultSeries";
import type { FilterValues } from "@/components/filters/filter-schema";

export const SIGNAL_FILTERS = defineFilters([
  {
    id: "sensor",
    label: "Sensor",
    kind: "select",
    pinned: true,
    required: true,
  },
  {
    id: "signal",
    label: "Signal",
    kind: "select",
    pinned: true,
    required: true,
  },
  {
    id: "window",
    label: "Time range",
    kind: "select",
    pinned: true,
    required: true,
  },
  {
    id: "group",
    debounceMs: 0,
    hideChip: true,
    label: "Group by attribute",
    kind: "text",
    placeholder: "department",
    description: "Any captured attribute key. Leave blank for all messages.",
  },
  { id: "step", label: "Interval", kind: "select", allLabel: "Automatic" },
  {
    id: "threshold",
    debounceMs: 0,
    label: "Minimum probability",
    kind: "number",
    min: 0.01,
    max: 1,
    step: 0.01,
    description:
      "Use a value greater than 0 and at most 1. Defaults to 0.5. Ordered levels follow each measured definition’s order; inspect definitions to interpret them.",
  },
  {
    id: "attribute",
    debounceMs: 0,
    label: "Filter attribute",
    kind: "text",
    placeholder: "model_provider",
  },
  {
    id: "comparison",
    label: "Attribute comparison",
    kind: "select",
    allLabel: "Equals",
  },
  {
    id: "value",
    debounceMs: 0,
    placeholder: "Exact value",
    label: "Exact attribute value",
    kind: "text",
    description:
      "Blank matches a present empty value. Missing and present comparisons take no value.",
  },
  {
    id: "run",
    debounceMs: 0,
    placeholder: "Run ID",
    label: "Manual evaluation run",
    kind: "text",
    description: "Optional run ID. Leave blank for streaming evaluations.",
  },
]);

export const WINDOW_OPTIONS = [
  { value: "6h", label: "Last 6 hours" },
  { value: "7d", label: "Last 7 days" },
  { value: "90d", label: "Last 90 days" },
  { value: "24mo", label: "Last 24 months" },
];

export type SignalControls = FilterValues<typeof SIGNAL_FILTERS>;

export type SignalChartMetric = "matched_messages" | "match_share";

export function signalPayload(
  catalog: AnalyticsSignalCatalog,
  values: SignalControls,
  now: Date,
): AnalyticsQueryPayload {
  const sensor =
    catalog.sensors.find((item) => item.id === values.sensor) ??
    catalog.sensors[0]!;
  const signalId = sensor.signalIds.includes(values.signal ?? "")
    ? values.signal!
    : sensor.signalIds[0]!;
  const to = new Date(now);
  to.setUTCSeconds(0, 0);
  const from = new Date(to);
  switch (values.window) {
    case "6h":
      from.setTime(to.getTime() - 6 * 3_600_000);
      break;
    case "90d":
      from.setUTCDate(from.getUTCDate() - 90);
      break;
    case "24mo":
      from.setUTCFullYear(from.getUTCFullYear() - 2);
      break;
    case null:
    default:
      from.setUTCDate(from.getUTCDate() - 7);
  }
  const selection = { sensorId: sensor.id, threshold: values.threshold ?? 0.5 };
  const operator = values.comparison ?? "equals";
  return {
    dataset: "signals",
    from,
    to,
    grain: values.step ?? (values.window === "24mo" ? "month" : "auto"),
    signalQuery: {
      selection:
        sensor.mode === "ordered_score"
          ? { ...selection, minimumLevel: Number(values.signal ?? 0) }
          : { ...selection, signalId },
      trigger: values.run ? "manual" : "stream",
      runId: values.run || undefined,
      groupBy: values.group ? [{ attribute: values.group }] : [],
      filters: values.attribute
        ? [
            {
              dimension: { attribute: values.attribute },
              operator,
              values: operator === "equals" ? [values.value] : [],
            },
          ]
        : [],
      groups: 5,
      points: 2000,
    },
  };
}

export function signalSeries(
  result: AnalyticsQueryResult,
  metric: string,
): SeriesSet {
  const buckets = [
    ...new Set(result.rows.map((row) => String(row.time_bucket))),
  ].sort();
  const index = new Map(buckets.map((at, i) => [at, i]));
  const series = (result.signalResult?.groups ?? []).map((group) => ({
    label: groupLabel(group.group_values),
    unit: metric.includes("share") ? "ratio" : "count",
    points: buckets.map((): number | null => null),
  }));
  const groups = new Map(
    (result.signalResult?.groups ?? []).map((group, i) => [
      group.series_id,
      series[i]!,
    ]),
  );
  for (const row of result.rows) {
    const at = index.get(String(row.time_bucket));
    const group = groups.get(row.series_id);
    if (at !== undefined && group && typeof row[metric] === "number")
      group.points[at] = row[metric];
  }
  const partialBuckets = new Set(
    result.rows
      .filter((row) => row.partial === true)
      .map((row) => String(row.time_bucket)),
  );
  return { buckets, series, hidden: 0, partialBuckets };
}

export function groupLabel(value: unknown): string {
  if (!Array.isArray(value) || value.length === 0) return "All messages";
  return value
    .map((item) => {
      if (item === null) return "Not recorded";
      if (item === "") return "Empty value";
      return String(item);
    })
    .join(" · ");
}

export function signalNumber(value: unknown, percent = false): string {
  if (typeof value !== "number") return "—";
  if (percent)
    return new Intl.NumberFormat("en-US", {
      style: "percent",
      maximumFractionDigits: 1,
    }).format(value);
  return new Intl.NumberFormat("en-US").format(value);
}
