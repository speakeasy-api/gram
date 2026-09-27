import type { AdminSpendBreakdownResponse } from "@gram/admin-client/models/components/adminspendbreakdownresponse";

import { formatExactUsd } from "./billingState";
import {
  METER_FAMILY_COLOR,
  formatMeterQuantity,
  meterGroupEnd,
  meterGroupStart,
  type MeterGranularity,
} from "./meterUsageUtils";

export type AdminSpendBreakdown = AdminSpendBreakdownResponse;
export type SpendProduct = AdminSpendBreakdown["products"][number];
export type SpendProductID = SpendProduct["id"];
export const SPEND_PRODUCT_COLOR: Record<SpendProductID, string> = {
  agent_session_storage: METER_FAMILY_COLOR.agent_session_storage,
  risk_content_scans: METER_FAMILY_COLOR.risk_content_scans,
  mcp_egress: METER_FAMILY_COLOR.mcp_bandwidth,
};

const EXACT_DECIMAL = /^([+-]?)(\d+)(?:\.(\d*))?$/;

function fractionalDigits(value: string): number {
  const match = EXACT_DECIMAL.exec(value.trim());
  if (!match) throw new Error(`Invalid exact decimal: ${value}`);
  return match[3]?.length ?? 0;
}

function decimalToScaledInteger(value: string, scale: number): bigint {
  const match = EXACT_DECIMAL.exec(value.trim());
  if (!match) throw new Error(`Invalid exact decimal: ${value}`);
  const [, sign, whole = "", fraction = ""] = match;
  const digits = `${whole}${fraction.padEnd(scale, "0")}`.replace(
    /^0+(?=\d)/,
    "",
  );
  const integer = BigInt(digits);
  return sign === "-" ? -integer : integer;
}

function scaledIntegerToDecimal(value: bigint, scale: number): string {
  const sign = value < 0n ? "-" : "";
  const digits = (value < 0n ? -value : value)
    .toString()
    .padStart(scale + 1, "0");
  if (scale === 0) return `${sign}${digits}`;
  const whole = digits.slice(0, -scale);
  const fraction = digits.slice(-scale).replace(/0+$/, "");
  return fraction.length === 0
    ? `${sign}${whole}`
    : `${sign}${whole}.${fraction}`;
}

export function validateSpendBreakdown(
  data: AdminSpendBreakdown,
): AdminSpendBreakdown {
  fractionalDigits(data.totalCostUsd);
  for (const product of data.products) {
    fractionalDigits(product.costUsd);
    fractionalDigits(product.rateUsd);
    BigInt(product.quantity);
    BigInt(product.rateQuantity);
    for (const bucket of product.buckets) {
      fractionalDigits(bucket.costUsd);
      BigInt(bucket.quantity);
    }
  }
  return data;
}

export function formatSpendUsd(value: string): string {
  const match = EXACT_DECIMAL.exec(value.trim());
  if (!match) return "—";
  const [, sign = "", whole = "0", fraction = ""] = match;
  const cents =
    BigInt(whole) * 100n +
    BigInt(fraction.padEnd(2, "0").slice(0, 2)) +
    ((fraction[2] ?? "0") >= "5" ? 1n : 0n);
  if (cents === 0n && /[1-9]/.test(fraction)) {
    return sign === "-" ? ">-$0.01" : "<$0.01";
  }
  return formatExactUsd(`${sign}${scaledIntegerToDecimal(cents, 2)}`) ?? "—";
}
export function sumSpendCosts(values: readonly string[]): string {
  const scale = values.reduce(
    (maximum, value) => Math.max(maximum, fractionalDigits(value)),
    0,
  );
  const total = values.reduce(
    (sum, value) => sum + decimalToScaledInteger(value, scale),
    0n,
  );
  return scaledIntegerToDecimal(total, scale);
}
export function spendCostIsZero(value: string): boolean {
  const scale = fractionalDigits(value);
  return decimalToScaledInteger(value, scale) === 0n;
}

export function formatSpendRate(product: SpendProduct): string {
  return `${formatSpendUsd(product.rateUsd)} per ${formatMeterQuantity(product.rateQuantity, product.unit)}`;
}

export function formatSpendUsage(product: SpendProduct): string {
  return formatMeterQuantity(product.quantity, product.unit);
}

export type SpendChartPoint = {
  from: string;
  to: string;
  productId: SpendProductID;
  productLabel: string;
  exactCostUsd: string;
  scaledCost: number;
  inProgress: boolean;
};

export type SpendChartData = {
  points: SpendChartPoint[];
  scale: number;
  maximum: number;
};

export function spendChartData(
  data: AdminSpendBreakdown,
  selectedProductIDs: ReadonlySet<SpendProductID>,
  granularity: MeterGranularity,
  cumulative: boolean,
): SpendChartData {
  const products = data.products.filter((product) =>
    selectedProductIDs.has(product.id),
  );
  const visibleCosts = products.flatMap((product) =>
    product.buckets
      .filter((bucket) => bucket.from.getTime() <= data.queriedAt.getTime())
      .map((bucket) => bucket.costUsd),
  );
  const scale = visibleCosts.reduce(
    (maximum, value) => Math.max(maximum, fractionalDigits(value)),
    0,
  );
  const points: SpendChartPoint[] = [];
  const totalsByStart = new Map<number, bigint>();

  for (const product of products) {
    const grouped = new Map<number, bigint>();
    for (const bucket of product.buckets) {
      const bucketFrom = bucket.from.getTime();
      if (bucketFrom > data.queriedAt.getTime()) continue;
      const start = meterGroupStart(bucketFrom, granularity);
      grouped.set(
        start,
        (grouped.get(start) ?? 0n) +
          decimalToScaledInteger(bucket.costUsd, scale),
      );
    }

    let running = 0n;
    for (const [start, groupCost] of [...grouped.entries()].sort(
      ([left], [right]) => left - right,
    )) {
      const cost = cumulative ? (running += groupCost) : groupCost;
      const end = Math.min(
        meterGroupEnd(start, granularity),
        data.window.to.getTime(),
      );
      totalsByStart.set(start, (totalsByStart.get(start) ?? 0n) + cost);
      points.push({
        from: new Date(
          Math.max(start, data.window.from.getTime()),
        ).toISOString(),
        to: new Date(end).toISOString(),
        productId: product.id,
        productLabel: product.label,
        exactCostUsd: scaledIntegerToDecimal(cost, scale),
        scaledCost: Number(cost),
        inProgress:
          start <= data.queriedAt.getTime() && end > data.queriedAt.getTime(),
      });
    }
  }

  let maximum = 1n;
  for (const total of totalsByStart.values()) {
    if (total > maximum) maximum = total;
  }
  return { points, scale, maximum: Number(maximum) };
}

export function formatScaledUsdAxis(value: number, scale: number): string {
  const exact = scaledIntegerToDecimal(BigInt(Math.round(value)), scale);
  const amount = Number(exact);
  if (!Number.isFinite(amount)) return "—";
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    notation: "compact",
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(amount);
}
