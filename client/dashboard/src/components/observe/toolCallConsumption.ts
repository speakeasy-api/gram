import {
  Dimension,
  type QueryFilter,
} from "@gram/client/models/components/queryfilter.js";
import { type QueryPayloadSortBy } from "@gram/client/models/components/querypayload.js";
import { type QueryMeasures } from "@gram/client/models/components/querymeasures.js";
import { type QueryRow } from "@gram/client/models/components/queryrow.js";
import { type QuerySeries } from "@gram/client/models/components/queryseries.js";
import { unixNanoToMs } from "@/components/chart/chartUtils";
import {
  OTHER_STACK_LABEL,
  type TimeSeriesStack,
} from "@/components/stacked-time-series";
import { displayName, isAttributionDim } from "@/pages/costs/taxonomy";

export type ConsumptionMeasure = "calls" | "tokens";

export type ConsumptionGroupBy =
  | typeof Dimension.HookSource
  | typeof Dimension.McpServerName
  | typeof Dimension.McpToolName;

export const CONSUMPTION_GROUP_OPTIONS: {
  value: ConsumptionGroupBy;
  label: string;
}[] = [
  { value: Dimension.HookSource, label: "Agent" },
  { value: Dimension.McpServerName, label: "MCP Server" },
  { value: Dimension.McpToolName, label: "MCP Tool" },
];

export type ConsumptionTotals = {
  toolCalls: number;
  inputTokens: number;
  outputTokens: number;
  tokens: number;
  cost: number;
};

export const EMPTY_CONSUMPTION_TOTALS: ConsumptionTotals = {
  toolCalls: 0,
  inputTokens: 0,
  outputTokens: 0,
  tokens: 0,
  cost: 0,
};

export function buildConsumptionFilters(
  projectId: string,
  hookSources: string[],
  accountType: string,
): QueryFilter[] {
  const filters: QueryFilter[] = [
    { dimension: Dimension.ProjectId, values: [projectId] },
  ];
  if (hookSources.length > 0) {
    filters.push({ dimension: Dimension.HookSource, values: hookSources });
  }
  if (accountType) {
    filters.push({ dimension: Dimension.AccountType, values: [accountType] });
  }
  return filters;
}

export function sortByForMeasure(
  measure: ConsumptionMeasure,
): QueryPayloadSortBy {
  return measure === "calls" ? "total_tool_calls" : "total_tokens";
}

export function measureValue(
  measures: QueryMeasures,
  measure: ConsumptionMeasure,
): number {
  return measure === "calls" ? measures.totalToolCalls : measures.totalTokens;
}

export function sumConsumptionRows(rows: QueryRow[]): ConsumptionTotals {
  return rows.reduce<ConsumptionTotals>(
    (acc, row) => ({
      toolCalls: acc.toolCalls + (row.measures.totalToolCalls ?? 0),
      inputTokens: acc.inputTokens + (row.measures.totalInputTokens ?? 0),
      outputTokens: acc.outputTokens + (row.measures.totalOutputTokens ?? 0),
      tokens: acc.tokens + (row.measures.totalTokens ?? 0),
      cost: acc.cost + (row.measures.totalCost ?? 0),
    }),
    { ...EMPTY_CONSUMPTION_TOTALS },
  );
}

export function hasConsumptionActivity(rows: QueryRow[]): boolean {
  return rows.some(
    (row) =>
      (row.measures.totalToolCalls ?? 0) > 0 ||
      (row.measures.totalTokens ?? 0) > 0 ||
      (row.measures.totalCost ?? 0) > 0,
  );
}

// Attribution cuts use "" for "not applicable" (the turn had no MCP tool),
// not an unset identity. Hide those from ranked charts/tables so they don't
// read as a real server or tool.
export function visibleConsumptionRows(
  rows: QueryRow[],
  groupBy: ConsumptionGroupBy,
): QueryRow[] {
  return rows.filter(
    (row) => !(isAttributionDim(groupBy) && row.groupValue === ""),
  );
}

export function consumptionRowLabel(
  groupBy: ConsumptionGroupBy,
  groupValue: string,
): string {
  if (groupValue === "Other") return "Other";
  return displayName(groupBy, groupValue);
}

const MAX_BAR_ROWS = 12;
const MAX_CHART_STACKS = 7;

export function rankedConsumptionRows(
  rows: QueryRow[],
  groupBy: ConsumptionGroupBy,
  measure: ConsumptionMeasure,
): QueryRow[] {
  return [...visibleConsumptionRows(rows, groupBy)].sort(
    (a, b) =>
      measureValue(b.measures, measure) - measureValue(a.measures, measure),
  );
}

export function consumptionBarRows(
  rows: QueryRow[],
  groupBy: ConsumptionGroupBy,
  measure: ConsumptionMeasure,
): { label: string; value: number; groupValue: string }[] {
  return rankedConsumptionRows(rows, groupBy, measure)
    .filter((row) => measureValue(row.measures, measure) > 0)
    .slice(0, MAX_BAR_ROWS)
    .map((row) => ({
      label: consumptionRowLabel(groupBy, row.groupValue),
      value: measureValue(row.measures, measure),
      groupValue: row.groupValue,
    }));
}

export function consumptionTimeSeriesStacks(
  series: QuerySeries[],
  groupBy: ConsumptionGroupBy,
  measure: ConsumptionMeasure,
  serverRollupValue?: string,
): { bucketsMs: number[]; stacks: TimeSeriesStack[] } {
  const bucketsMs = (series[0]?.points ?? []).map((point) =>
    unixNanoToMs(point.bucketTimeUnixNano),
  );

  const visible = series.filter(
    (item) => !(isAttributionDim(groupBy) && item.groupValue === ""),
  );

  const byLabel = new Map<string, number[]>();
  let rollupSeed: number[] | null = null;
  for (const item of visible) {
    const values = item.points.map((point) =>
      measureValue(point.measures, measure),
    );
    if (item.groupValue === serverRollupValue) {
      rollupSeed = values;
      continue;
    }
    const label = consumptionRowLabel(groupBy, item.groupValue);
    const merged = byLabel.get(label);
    if (merged) {
      values.forEach((value, index) => {
        merged[index] = (merged[index] ?? 0) + value;
      });
    } else {
      byLabel.set(label, values);
    }
  }

  const all = [...byLabel.entries()]
    .map(([label, values]) => ({
      label,
      series: values,
      total: values.reduce((sum, value) => sum + value, 0),
    }))
    .filter((item) => item.total > 0)
    .sort((a, b) => b.total - a.total)
    .map(({ label, series: values }) => ({ label, series: values }));

  if (rollupSeed === null && all.length <= MAX_CHART_STACKS + 1) {
    return { bucketsMs, stacks: all };
  }

  const kept = all.slice(0, MAX_CHART_STACKS);
  const rest = all.slice(MAX_CHART_STACKS);
  let rollupLabel = OTHER_STACK_LABEL;
  while (byLabel.has(rollupLabel)) rollupLabel += " (other)";

  return {
    bucketsMs,
    stacks: [
      ...kept,
      {
        label: rollupLabel,
        rollup: true,
        series: bucketsMs.map(
          (_, index) =>
            (rollupSeed?.[index] ?? 0) +
            rest.reduce((sum, item) => sum + (item.series[index] ?? 0), 0),
        ),
      },
    ],
  };
}
