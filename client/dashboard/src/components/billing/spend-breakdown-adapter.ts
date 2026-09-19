import { type TimeSeriesStack } from "@/components/stacked-time-series";
import { formatExactUsd } from "./payg-billing-estimate";
import { formatMeterQuantity } from "./meter-usage-adapter";
import { type SpendBreakdownResponse } from "@gram/client/models/components/spendbreakdownresponse.js";

export type SpendBreakdownData = SpendBreakdownResponse;
export type SpendProduct = SpendBreakdownData["products"][number];
export type SpendProductID = SpendProduct["id"];

const PALETTE_INDEX_BY_PRODUCT: Record<SpendProductID, number> = {
  agent_session_storage: 0,
  risk_content_scans: 1,
  mcp_egress: 2,
};
const EXACT_DECIMAL = /^([+-]?)(\d+)(?:\.(\d*))?$/;

function fractionalDigits(value: string): number {
  const match = EXACT_DECIMAL.exec(value.trim());
  if (!match) throw new Error(`Invalid exact decimal: ${value}`);
  return match[3]?.length ?? 0;
}

/** Reject malformed API costs before chart or total arithmetic can run. */
export function validateSpendBreakdown(
  data: SpendBreakdownData,
): SpendBreakdownData {
  fractionalDigits(data.totalCostUsd);
  for (const product of data.products) {
    fractionalDigits(product.costUsd);
    for (const bucket of product.buckets) {
      fractionalDigits(bucket.costUsd);
    }
  }
  return data;
}

function decimalToScaledInteger(value: string, scale: number): bigint {
  const match = EXACT_DECIMAL.exec(value.trim());
  if (!match) throw new Error(`Invalid exact decimal: ${value}`);
  const [, sign, whole, fraction = ""] = match;
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

export type SpendChartData = {
  bucketsMs: number[];
  bucketEndsMs: number[];
  stacks: TimeSeriesStack[];
  decimalScale: number;
  queriedAtMs: number;
};

export function adaptSpendChart(
  data: SpendBreakdownData | undefined,
  selectedProducts: ReadonlySet<SpendProductID>,
): SpendChartData {
  if (!data) {
    return {
      bucketsMs: [],
      bucketEndsMs: [],
      stacks: [],
      decimalScale: 0,
      queriedAtMs: 0,
    };
  }

  const queriedAtMs = data.queriedAt.getTime();
  const products = data.products.filter((product) =>
    selectedProducts.has(product.id),
  );
  const observedBucketCount = data.products[0]?.buckets.findIndex(
    (bucket) => bucket.from.getTime() > queriedAtMs,
  );
  const bucketCount =
    observedBucketCount === -1 || observedBucketCount === undefined
      ? (data.products[0]?.buckets.length ?? 0)
      : observedBucketCount;
  const costValues = products.flatMap((product) =>
    product.buckets.slice(0, bucketCount).map((bucket) => bucket.costUsd),
  );
  const decimalScale = costValues.reduce(
    (maximum, value) => Math.max(maximum, fractionalDigits(value)),
    0,
  );
  const referenceBuckets =
    data.products[0]?.buckets.slice(0, bucketCount) ?? [];

  return {
    bucketsMs: referenceBuckets.map((bucket) => bucket.from.getTime()),
    bucketEndsMs: referenceBuckets.map((bucket) => bucket.to.getTime()),
    queriedAtMs,
    decimalScale,
    stacks: products.map((product) => ({
      key: product.id,
      label: product.label,
      paletteIndex: PALETTE_INDEX_BY_PRODUCT[product.id],
      series: product.buckets
        .slice(0, bucketCount)
        .map((bucket) => Number(bucket.costUsd)),
      exactSeries: product.buckets
        .slice(0, bucketCount)
        .map((bucket) =>
          decimalToScaledInteger(bucket.costUsd, decimalScale).toString(),
        ),
    })),
  };
}

export function sumSelectedCost(
  products: SpendProduct[],
  selectedProducts: ReadonlySet<SpendProductID>,
): string {
  const selected = products.filter((product) =>
    selectedProducts.has(product.id),
  );
  const scale = selected.reduce(
    (maximum, product) => Math.max(maximum, fractionalDigits(product.costUsd)),
    0,
  );
  const total = selected.reduce(
    (sum, product) => sum + decimalToScaledInteger(product.costUsd, scale),
    0n,
  );
  return scaledIntegerToDecimal(total, scale);
}

/** Round only presentation; aggregation retains the server's full precision. */
export function formatSpendUsd(value: string): string {
  if (!EXACT_DECIMAL.test(value.trim())) return "—";
  const scale = Math.max(2, fractionalDigits(value));
  const amount = decimalToScaledInteger(value, scale);
  const magnitude = amount < 0n ? -amount : amount;
  const divisor = 10n ** BigInt(scale - 2);
  if (magnitude > 0n && magnitude < divisor) {
    return amount < 0n ? ">-$0.01" : "<$0.01";
  }
  const cents = (magnitude + divisor / 2n) / divisor;
  const rounded = amount < 0n ? -cents : cents;
  return formatExactUsd(scaledIntegerToDecimal(rounded, 2)) ?? "—";
}

export function formatScaledUsd(value: string, scale: number): string {
  return formatSpendUsd(scaledIntegerToDecimal(BigInt(value), scale));
}

export function formatScaledUsdAxis(value: number, scale: number): string {
  const exact = scaledIntegerToDecimal(BigInt(Math.round(value)), scale);
  const amount = Number(exact);
  if (!Number.isFinite(amount)) return "—";
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    notation: "compact",
    maximumFractionDigits: amount < 1 ? 2 : 1,
  }).format(amount);
}

export function formatSpendRate(product: SpendProduct): string {
  const rate = formatExactUsd(product.rateUsd) ?? "—";
  return `${rate} per ${formatMeterQuantity(product.rateQuantity, product.unit)}`;
}

export function formatSpendUsage(product: SpendProduct): string {
  return formatMeterQuantity(product.quantity, product.unit, "standard");
}
