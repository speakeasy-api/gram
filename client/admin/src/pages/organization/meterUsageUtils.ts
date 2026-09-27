import type { AdminMeterUsageResponse } from "@gram/admin-client/models/components/adminmeterusageresponse";
import { scaleLinear } from "@tanstack/charts/scales/linear";

export type AdminMeterUsage = AdminMeterUsageResponse;
export type MeterFamily = AdminMeterUsage["family"];
export type MeterGranularity = "daily" | "weekly" | "monthly";

export const METER_FAMILY_COLOR: Record<MeterFamily, string> = {
  agent_session_storage: "var(--billing-storage)",
  risk_content_scans: "var(--billing-risk)",
  mcp_bandwidth: "var(--billing-mcp)",
};

export type MeterPoint = {
  from: string;
  to: string;
  total: string;
  value: number;
};

const MS_PER_DAY = 86_400_000;
const exactInteger = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 0,
});
const meterDate = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  timeZone: "UTC",
});
const TOKEN_UNITS = ["tokens", "KTok", "MTok", "BTok"] as const;
const BYTE_UNITS = ["bytes", "KiB", "MiB", "GiB"] as const;

export function meterGroupStart(
  ms: number,
  granularity: MeterGranularity,
): number {
  const date = new Date(ms);
  const day = Date.UTC(
    date.getUTCFullYear(),
    date.getUTCMonth(),
    date.getUTCDate(),
  );

  switch (granularity) {
    case "daily":
      return day;
    case "weekly":
      return day - ((date.getUTCDay() + 6) % 7) * MS_PER_DAY;
    case "monthly":
      return Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), 1);
  }
}

export function meterGroupEnd(
  ms: number,
  granularity: MeterGranularity,
): number {
  const date = new Date(ms);
  switch (granularity) {
    case "daily":
      return ms + MS_PER_DAY;
    case "weekly":
      return ms + 7 * MS_PER_DAY;
    case "monthly":
      return Date.UTC(date.getUTCFullYear(), date.getUTCMonth() + 1, 1);
  }
}

export function meterPoints(
  data: AdminMeterUsage,
  granularity: MeterGranularity,
  cumulative: boolean,
): MeterPoint[] {
  const reportFrom = data.window.from.getTime();
  const reportTo = data.window.to.getTime();
  const effectiveTo = cumulative
    ? Math.min(reportTo, data.queriedAt.getTime())
    : reportTo;
  const totals = new Map<number, bigint>();

  for (const bucket of data.buckets) {
    const bucketFrom = bucket.from.getTime();
    const bucketTo = bucket.to.getTime();
    if (bucketFrom >= effectiveTo || bucketTo <= reportFrom) continue;

    const start = meterGroupStart(bucketFrom, granularity);
    totals.set(start, (totals.get(start) ?? 0n) + BigInt(bucket.total));
  }

  let running = 0n;
  return [...totals.entries()]
    .sort(([left], [right]) => left - right)
    .map(([start, groupTotal]) => {
      const total = cumulative ? (running += groupTotal) : groupTotal;
      return {
        from: new Date(Math.max(start, reportFrom)).toISOString(),
        to: new Date(
          Math.min(meterGroupEnd(start, granularity), effectiveTo),
        ).toISOString(),
        total: total.toString(),
        value: Number(total),
      };
    });
}

function formatMeterRatio(
  value: bigint,
  denominator: bigint,
  unit: string,
): string {
  const units = unit === "bytes" ? BYTE_UNITS : TOKEN_UNITS;
  const base = unit === "bytes" ? 1024n : 1000n;
  const absolute = value < 0n ? -value : value;
  const numerator = absolute * 10n;
  let divisor = denominator;
  let scale = 0;
  let tenths = (numerator + divisor / 2n) / divisor;

  while (scale < units.length - 1 && tenths >= base * 10n) {
    divisor *= base;
    scale++;
    tenths = (numerator + divisor / 2n) / divisor;
  }

  const whole = exactInteger.format(tenths / 10n);
  const fraction = tenths % 10n === 0n ? "" : `.${tenths % 10n}`;
  const sign = value < 0n && tenths !== 0n ? "-" : "";
  return `${sign}${whole}${fraction} ${units[scale]}`;
}

export function formatMeterQuantity(total: string, unit: string): string {
  return formatMeterRatio(BigInt(total), 1n, unit);
}

export function meterAxisTicks(maxValue: number): number[] {
  return scaleLinear()
    .domain([0, Math.max(1, maxValue)])
    .nice(5)
    .ticks(5)
    .filter(Number.isInteger);
}

export function formatDailyMeterRate(data: AdminMeterUsage): string {
  const from = data.window.from.getTime();
  const to = Math.min(data.window.to.getTime(), data.queriedAt.getTime());
  const elapsed = to - from;
  if (elapsed <= 0) return "—";

  const rate = formatMeterRatio(
    BigInt(data.total) * BigInt(MS_PER_DAY),
    BigInt(elapsed),
    data.unit,
  );
  return `${rate}/day`;
}

export function meterDateLabel(date: Date | string): string {
  return meterDate.format(typeof date === "string" ? new Date(date) : date);
}
