import type { TimeseriesSeries } from "@/components/chart/Timeseries";
import type { TimeSeriesBucket } from "@gram/client/models/components/timeseriesbucket.js";

export type AgentTokenValueMode = "tokens" | "cost";

type AgentTokenTimeSeriesBucket = Pick<
  TimeSeriesBucket,
  | "bucketTimeUnixNano"
  | "totalCost"
  | "totalInputTokens"
  | "totalOutputTokens"
  | "cacheReadInputTokens"
>;

function unixNanoToMillis(value: string): number {
  return Number(BigInt(value) / 1_000_000n);
}

/**
 * Shapes token/cost buckets into the series `<Timeseries mode="bar-with-trend">`
 * expects. Only the per-bucket bars are built here — the smoothed trend
 * overlay is computed by Timeseries itself (it sums every series' y value per
 * bucket and smooths the total), so callers don't shape a trend dataset.
 */
export function buildAgentTokenTimeSeries(
  timeSeries: AgentTokenTimeSeriesBucket[],
  valueMode: AgentTokenValueMode,
): TimeseriesSeries[] {
  const points = timeSeries.map((bucket) => ({
    x: unixNanoToMillis(bucket.bucketTimeUnixNano),
    bucket,
  }));

  if (valueMode === "cost") {
    return [
      {
        label: "Cost",
        data: points.map(({ x, bucket }) => ({ x, y: bucket.totalCost })),
      },
    ];
  }

  return [
    {
      label: "Input Tokens",
      data: points.map(({ x, bucket }) => ({ x, y: bucket.totalInputTokens })),
    },
    {
      label: "Output Tokens",
      data: points.map(({ x, bucket }) => ({
        x,
        y: bucket.totalOutputTokens,
      })),
    },
    {
      label: "Cache Read",
      data: points.map(({ x, bucket }) => ({
        x,
        y: bucket.cacheReadInputTokens,
      })),
    },
  ];
}
