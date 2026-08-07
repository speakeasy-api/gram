import { formatChartLabel, smoothData } from "@/components/chart/chartUtils";
import { SERIES, withAlpha } from "@/components/chart/palette";
import type { TimeSeriesBucket } from "@gram/client/models/components/timeseriesbucket.js";
import type { ChartDataset } from "chart.js";

export type AgentTokenValueMode = "tokens" | "cost";

type AgentTokenTimeSeriesBucket = Pick<
  TimeSeriesBucket,
  | "bucketTimeUnixNano"
  | "totalCost"
  | "totalInputTokens"
  | "totalOutputTokens"
  | "cacheReadInputTokens"
>;

export type AgentTokenTimeSeriesChartData = {
  labels: string[];
  datasets: Array<
    ChartDataset<"bar", number[]> | ChartDataset<"line", number[]>
  >;
};

function unixNanoToMillis(value: string): number {
  return Number(BigInt(value) / 1_000_000n);
}

export function buildAgentTokenTimeSeriesChartData(
  timeSeries: AgentTokenTimeSeriesBucket[],
  timeRangeMs: number,
  valueMode: AgentTokenValueMode,
  // Theme-resolved series ramp; component callers pass seriesForTheme(isDark)
  // so dark mode lifts the near-black entries. Defaults to the light ramp.
  colors: readonly string[] = SERIES,
): {
  timestamps: number[];
  chartData: AgentTokenTimeSeriesChartData;
} {
  const timestamps = timeSeries.map((bucket) =>
    unixNanoToMillis(bucket.bucketTimeUnixNano),
  );
  const labels = timestamps.map((timestamp) =>
    formatChartLabel(new Date(timestamp), timeRangeMs),
  );

  // Bars step through the shared neutral ramp (at reduced alpha so the trend
  // line stays legible on top) — no hue coding.
  const barDatasets =
    valueMode === "cost"
      ? [
          {
            label: "Cost",
            data: timeSeries.map((bucket) => bucket.totalCost),
            backgroundColor: withAlpha(colors[1]!, 0.35),
            stack: "stack",
            order: 2,
          },
        ]
      : [
          {
            label: "Input Tokens",
            data: timeSeries.map((bucket) => bucket.totalInputTokens),
            backgroundColor: withAlpha(colors[1]!, 0.35),
            stack: "stack",
            order: 2,
          },
          {
            label: "Output Tokens",
            data: timeSeries.map((bucket) => bucket.totalOutputTokens),
            backgroundColor: withAlpha(colors[3]!, 0.35),
            stack: "stack",
            order: 2,
          },
          {
            label: "Cache Read",
            data: timeSeries.map((bucket) => bucket.cacheReadInputTokens),
            backgroundColor: withAlpha(colors[5]!, 0.35),
            stack: "stack",
            order: 2,
          },
        ];

  const rawTotal = timeSeries.map((bucket) =>
    valueMode === "cost"
      ? bucket.totalCost
      : bucket.totalInputTokens +
        bucket.totalOutputTokens +
        bucket.cacheReadInputTokens,
  );

  const trendDataset: ChartDataset<"line", number[]> = {
    label: valueMode === "cost" ? "Cost Trend" : "Token Trend",
    data: smoothData(rawTotal),
    type: "line",
    // Editorial trend line: ink for tokens, mid neutral for cost.
    borderColor: valueMode === "cost" ? colors[3]! : colors[0]!,
    backgroundColor: "transparent",
    pointRadius: 0,
    pointHoverRadius: 4,
    borderWidth: 2,
    tension: 0.4,
    fill: false,
    order: 1,
  };

  return {
    timestamps,
    chartData: {
      labels,
      datasets: [...barDatasets, trendDataset],
    },
  };
}
