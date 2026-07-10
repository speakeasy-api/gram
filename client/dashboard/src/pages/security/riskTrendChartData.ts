import type { TimeseriesSeries } from "@/components/chart/Timeseries";
import { RULE_CATEGORY_META, type RuleCategory } from "./policy-data";

// Stable display order for risk categories. Keeps the palette assignment (by
// series index, from chart-theme's seriesPalette()) and legend order
// consistent across renders, matching the priority order categories are
// surfaced in throughout the policy UI.
const RISK_CATEGORY_ORDER: ReadonlyArray<RuleCategory> = [
  "secrets",
  "financial",
  "pii",
  "government_ids",
  "healthcare",
  "prompt_policy",
  "prompt_injection",
  "off_policy",
  "shadow_mcp",
  "destructive_tool",
  "cli_destructive",
  "account_identity",
  "custom",
];

const RISK_CATEGORY_ORDER_INDEX = new Map<RuleCategory, number>(
  RISK_CATEGORY_ORDER.map((category, index) => [category, index]),
);

export type TrendPoint = {
  category: string;
  bucketStart: Date;
  findings: number;
};

/** Shapes risk-finding buckets into one `<Timeseries>` series per category. */
export function buildRiskTrendSeries(points: TrendPoint[]): TimeseriesSeries[] {
  const seriesMap = new Map<string, Map<number, number>>();

  for (const point of points) {
    const timestamp = point.bucketStart.getTime();
    const series = seriesMap.get(point.category) ?? new Map<number, number>();
    series.set(timestamp, point.findings);
    seriesMap.set(point.category, series);
  }

  return Array.from(seriesMap.entries())
    .sort(([left], [right]) => {
      const leftOrder =
        RISK_CATEGORY_ORDER_INDEX.get(left as RuleCategory) ??
        Number.MAX_SAFE_INTEGER;
      const rightOrder =
        RISK_CATEGORY_ORDER_INDEX.get(right as RuleCategory) ??
        Number.MAX_SAFE_INTEGER;
      return leftOrder - rightOrder || left.localeCompare(right);
    })
    .map(([category, series]) => {
      const meta = RULE_CATEGORY_META[category as RuleCategory];
      return {
        label: meta?.label ?? category,
        data: Array.from(series.entries()).map(([x, y]) => ({ x, y })),
      };
    });
}
