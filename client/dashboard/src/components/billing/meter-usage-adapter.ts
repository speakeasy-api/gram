import { type TimeSeriesStack } from "@/components/stacked-time-series";
import { type MeterUsageResponse } from "@gram/client/models/components/meterusageresponse.js";

export type MeterUsageData = MeterUsageResponse;

export type MeterChartData = {
  bucketsMs: number[];
  bucketEndsMs: number[];
  stacks: TimeSeriesStack[];
};

export function meterSeriesIdentity(
  series: MeterUsageResponse["breakdown"]["series"][number],
): string {
  return `${series.kind}:${series.key ?? ""}`;
}

export function adaptMeterChart(
  data: MeterUsageData | undefined,
): MeterChartData {
  if (!data) return { bucketsMs: [], bucketEndsMs: [], stacks: [] };
  return {
    bucketsMs: data.buckets.map((bucket) => bucket.from.getTime()),
    bucketEndsMs: data.buckets.map((bucket) => bucket.to.getTime()),
    stacks: data.breakdown.series.map((series) => ({
      key: meterSeriesIdentity(series),
      label: series.label,
      series: series.values.map(Number),
      exactSeries: series.values,
      rollup: series.kind === "remainder" || undefined,
    })),
  };
}

const exactInteger = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 0,
});
const compactInteger = new Intl.NumberFormat("en-US", {
  notation: "compact",
  maximumFractionDigits: 1,
});

export function formatExactInteger(value: string): string {
  return exactInteger.format(BigInt(value));
}

export function formatMeterQuantity(value: string, unit: string): string {
  const suffix = unit === "bytes" ? "bytes" : "s-tokens";
  return `${formatExactInteger(value)} ${suffix}`;
}
export function formatDailyMeterRate(
  total: string,
  unit: string,
  from: Date,
  to: Date,
  now: Date,
): string {
  const elapsedMs = Math.min(to.getTime(), now.getTime()) - from.getTime();
  if (elapsedMs <= 0) return "—";
  const value = BigInt(total);
  const sign = value < 0n ? "-" : "";
  const absolute = value < 0n ? -value : value;
  const dayMs = 86_400_000n;
  const elapsed = BigInt(elapsedMs);
  const tenths = (absolute * dayMs * 10n + elapsed / 2n) / elapsed;
  const whole = tenths / 10n;
  const decimal = tenths % 10n;
  const suffix = unit === "bytes" ? "bytes/day" : "s-tokens/day";
  return `${sign}${exactInteger.format(whole)}.${decimal} ${suffix}`;
}

export function formatMeterAxis(value: number): string {
  return compactInteger.format(value);
}
