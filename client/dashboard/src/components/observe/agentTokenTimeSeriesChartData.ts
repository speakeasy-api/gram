import { formatChartLabel, smoothData } from "@/components/chart/chartUtils";
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

  const barDatasets =
    valueMode === "cost"
      ? [
          {
            label: "Cost",
            data: timeSeries.map((bucket) => bucket.totalCost),
            backgroundColor: "rgba(96, 165, 250, 0.35)",
            stack: "stack",
            order: 2,
          },
        ]
      : [
          {
            label: "Input Tokens",
            data: timeSeries.map((bucket) => bucket.totalInputTokens),
            backgroundColor: "rgba(96, 165, 250, 0.35)",
            stack: "stack",
            order: 2,
          },
          {
            label: "Output Tokens",
            data: timeSeries.map((bucket) => bucket.totalOutputTokens),
            backgroundColor: "rgba(52, 211, 153, 0.35)",
            stack: "stack",
            order: 2,
          },
          {
            label: "Cache Read",
            data: timeSeries.map((bucket) => bucket.cacheReadInputTokens),
            backgroundColor: "rgba(167, 139, 250, 0.35)",
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
    borderColor: valueMode === "cost" ? "#818cf8" : "#3b82f6",
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
