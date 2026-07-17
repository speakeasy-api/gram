import { formatChartLabel } from "@/components/chart/chartUtils";
import type { ChartDataset } from "chart.js";
import { RULE_CATEGORY_META, type RuleCategory } from "./policy-data";

type TimestampedLineDataset = ChartDataset<
  "line",
  Array<{ x: number; y: number }>
>;

const RISK_CATEGORY_CHART_COLORS = [
  { category: "secrets", color: "#60a5fa" },
  { category: "financial", color: "#34d399" },
  { category: "pii", color: "#f87171" },
  { category: "government_ids", color: "#a78bfa" },
  { category: "healthcare", color: "#facc15" },
  { category: "prompt_policy", color: "#38bdf8" },
  { category: "prompt_injection", color: "#22d3ee" },
  { category: "off_policy", color: "#f472b6" },
  { category: "shadow_mcp", color: "#a3e635" },
  { category: "destructive_tool", color: "#818cf8" },
  { category: "cli_destructive", color: "#fb7185" },
  { category: "account_identity", color: "#fb923c" },
  { category: "custom", color: "#94a3b8" },
] satisfies ReadonlyArray<{ category: RuleCategory; color: string }>;

const RISK_CATEGORY_CHART_COLOR_BY_CATEGORY = new Map<RuleCategory, string>(
  RISK_CATEGORY_CHART_COLORS.map(({ category, color }) => [category, color]),
);

const RISK_CATEGORY_CHART_ORDER = new Map<RuleCategory, number>(
  RISK_CATEGORY_CHART_COLORS.map(({ category }, index) => [category, index]),
);

export type TrendPoint = {
  category: string;
  bucketStart: Date;
  findings: number;
};

function getRiskCategoryChartColor(category: string) {
  return RISK_CATEGORY_CHART_COLOR_BY_CATEGORY.get(category as RuleCategory);
}

export function buildRiskTrendChartData(
  points: TrendPoint[],
  from: Date,
  to: Date,
): {
  timestamps: number[];
  labels: string[];
  tooltipLabels: string[];
  datasets: TimestampedLineDataset[];
} {
  if (points.length === 0) {
    return { timestamps: [], labels: [], tooltipLabels: [], datasets: [] };
  }

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
        getRiskCategoryChartColor(category) ??
        RISK_CATEGORY_CHART_COLORS[index % RISK_CATEGORY_CHART_COLORS.length]!
          .color;
      const meta = RULE_CATEGORY_META[category as RuleCategory];
      return {
        label: meta?.label ?? category,
        data: timestamps.map((timestamp) => ({
          x: timestamp,
          y: series.get(timestamp) ?? 0,
        })),
        borderColor: color,
        backgroundColor: `${color}1a`,
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
