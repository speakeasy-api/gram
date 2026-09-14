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

export function meterSeriesLabel(
  series: MeterUsageResponse["breakdown"]["series"][number],
  dimension: string,
  projectSlugs: ReadonlyMap<string, string>,
): string {
  if (series.kind === "unset") return "(unset)";
  if (dimension === "project" && series.kind === "value" && series.key) {
    return projectSlugs.get(series.key) || series.key;
  }
  return series.label;
}

export function adaptMeterChart(
  data: MeterUsageData | undefined,
  projectSlugs: ReadonlyMap<string, string>,
): MeterChartData {
  if (!data) return { bucketsMs: [], bucketEndsMs: [], stacks: [] };
  return {
    bucketsMs: data.buckets.map((bucket) => bucket.from.getTime()),
    bucketEndsMs: data.buckets.map((bucket) => bucket.to.getTime()),
    stacks: data.breakdown.series.map((series) => ({
      key: meterSeriesIdentity(series),
      label: meterSeriesLabel(series, data.breakdown.dimension, projectSlugs),
      series: series.values.map(Number),
      exactSeries: series.values,
      rollup: series.kind === "remainder" || undefined,
    })),
  };
}

const exactInteger = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 0,
});
const TOKEN_UNITS = ["tokens", "KTok", "MTok", "BTok"] as const;
const BYTE_UNITS = ["bytes", "KiB", "MiB", "GiB"] as const;
type MeterNotation = "compact" | "standard";

function formatMeterRatio(
  value: bigint,
  denominator: bigint,
  unit: string,
  notation: MeterNotation,
): string {
  const units = unit === "bytes" ? BYTE_UNITS : TOKEN_UNITS;
  const base = unit === "bytes" ? 1024n : 1000n;
  const absolute = value < 0n ? -value : value;
  const numerator = absolute * 10n;
  let divisor = denominator;
  let scale = 0;
  let tenths = (numerator + divisor / 2n) / divisor;
  // Promote after rounding so a boundary displays 1 MTok, not 1,000 KTok.
  while (
    notation === "compact" &&
    scale < units.length - 1 &&
    tenths >= base * 10n
  ) {
    divisor *= base;
    scale++;
    tenths = (numerator + divisor / 2n) / divisor;
  }
  const whole = exactInteger.format(tenths / 10n);
  const decimal = tenths % 10n;
  const fraction = decimal === 0n ? "" : `.${decimal}`;
  const sign = value < 0n && tenths !== 0n ? "-" : "";
  return `${sign}${whole}${fraction} ${units[scale]}`;
}

export function formatMeterQuantity(
  value: string,
  unit: string,
  notation: MeterNotation = "compact",
): string {
  return formatMeterRatio(BigInt(value), 1n, unit, notation);
}
export function formatDailyMeterRate(
  total: string,
  unit: string,
  from: Date,
  to: Date,
  now: Date,
  notation: MeterNotation = "compact",
): string {
  const elapsedMs = Math.min(to.getTime(), now.getTime()) - from.getTime();
  if (elapsedMs <= 0) return "—";
  const perDay = formatMeterRatio(
    BigInt(total) * 86_400_000n,
    BigInt(elapsedMs),
    unit,
    notation,
  );
  return `${perDay}/day`;
}

export function formatMeterAxis(value: number, unit: string): string {
  return formatMeterRatio(BigInt(Math.round(value * 10)), 10n, unit, "compact");
}
