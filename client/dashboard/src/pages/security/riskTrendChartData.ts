import { formatChartLabel } from "@/components/chart/chartUtils";
import {
  ACCENT_RED,
  OTHER_SERIES,
  SERIES,
  SEVERITY,
} from "@/components/chart/palette";
import type { ChartDataset } from "chart.js";
import { RULE_CATEGORY_META, type RuleCategory } from "./policy-data";

type TimestampedLineDataset = ChartDataset<
  "line",
  Array<{ x: number; y: number }>
>;

// Editorial severity-first ramp from the shared chart palette: the worst
// category (secrets) takes the one red accent, the next tiers take the
// severity oranges, the rest walk the neutral ink ramp (lightness repeats are
// fine — lines read by legend label, not hue), and "custom" recedes to the
// Other neutral.
function riskCategoryChartColors(
  series: readonly string[],
): Array<{ category: RuleCategory; color: string }> {
  return [
    { category: "secrets", color: ACCENT_RED },
    { category: "financial", color: SEVERITY.high },
    { category: "pii", color: SEVERITY.medium },
    { category: "government_ids", color: series[0]! },
    { category: "healthcare", color: series[1]! },
    { category: "prompt_policy", color: series[2]! },
    { category: "prompt_injection", color: series[3]! },
    { category: "off_policy", color: series[4]! },
    { category: "shadow_mcp", color: series[5]! },
    { category: "destructive_tool", color: series[1]! },
    { category: "cli_destructive", color: series[2]! },
    { category: "account_identity", color: series[3]! },
    { category: "custom", color: OTHER_SERIES },
  ];
}

// Category ordering is color-independent, so it is derived once.
const RISK_CATEGORY_CHART_ORDER = new Map<RuleCategory, number>(
  riskCategoryChartColors(SERIES).map(({ category }, index) => [
    category,
    index,
  ]),
);

export type TrendPoint = {
  category: string;
  bucketStart: Date;
  findings: number;
};

// Resolves one category's chart color from the theme-resolved series ramp
// (callers pass useSeriesColors()), so the Watchdog exposure bar shares the
// trend chart's palette in both themes.
export function getRiskCategoryChartColor(
  category: string,
  series: readonly string[],
): string | undefined {
  return riskCategoryChartColors(series).find(
    (entry) => entry.category === category,
  )?.color;
}

export function buildRiskTrendChartData(
  points: TrendPoint[],
  from: Date,
  to: Date,
  // Theme-resolved series ramp; component callers pass seriesForTheme(isDark)
  // so dark mode lifts the near-black entries. Defaults to the light ramp.
  seriesColors: readonly string[] = SERIES,
): {
  timestamps: number[];
  labels: string[];
  tooltipLabels: string[];
  datasets: TimestampedLineDataset[];
} {
  if (points.length === 0) {
    return { timestamps: [], labels: [], tooltipLabels: [], datasets: [] };
  }

  const categoryColors = riskCategoryChartColors(seriesColors);
  const colorByCategory = new Map<RuleCategory, string>(
    categoryColors.map(({ category, color }) => [category, color]),
  );

  const timeRangeMs = to.getTime() - from.getTime();
  const dateMap = new Map<number, Date>();
  const seriesMap = new Map<string, Map<number, number>>();

  for (const point of points) {
    const timestamp = point.bucketStart.getTime();
    dateMap.set(timestamp, point.bucketStart);
    const series = seriesMap.get(point.category) ?? new Map<number, number>();
    series.set(timestamp, point.findings);
    seriesMap.set(point.category, series);
  }

  const timestamps = Array.from(dateMap.keys()).sort((a, b) => a - b);
  const labels = timestamps.map((timestamp) =>
    formatChartLabel(dateMap.get(timestamp)!, timeRangeMs),
  );
  const tooltipLabels = timestamps.map((timestamp) =>
    dateMap.get(timestamp)!.toLocaleString([], {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    }),
  );

  const datasets = Array.from(seriesMap.entries())
    .sort(([left], [right]) => {
      const leftOrder =
        RISK_CATEGORY_CHART_ORDER.get(left as RuleCategory) ??
        Number.MAX_SAFE_INTEGER;
      const rightOrder =
        RISK_CATEGORY_CHART_ORDER.get(right as RuleCategory) ??
        Number.MAX_SAFE_INTEGER;

      return leftOrder - rightOrder || left.localeCompare(right);
    })
    .map(([category, series], index): TimestampedLineDataset => {
      const color =
        colorByCategory.get(category as RuleCategory) ??
        categoryColors[index % categoryColors.length]!.color;
      const meta = RULE_CATEGORY_META[category as RuleCategory];
      return {
        label: meta?.label ?? category,
        data: timestamps.map((timestamp) => ({
          x: timestamp,
          y: series.get(timestamp) ?? 0,
        })),
        borderColor: color,
        // No alpha area wash — the editorial style keeps line charts flat.
        backgroundColor: "transparent",
        pointBackgroundColor: color,
        fill: false,
        tension: 0.45,
        borderWidth: 1.5,
        pointRadius: 0,
        pointHoverRadius: 4,
      };
    });

  return {
    timestamps,
    labels,
    tooltipLabels,
    datasets,
  };
}
