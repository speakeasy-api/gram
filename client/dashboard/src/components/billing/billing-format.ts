export type BillingUnit = "chat credits" | "servers" | "tool calls";

const quantityFormatter = new Intl.NumberFormat("en-US", {
  maximumFractionDigits: 2,
});

const currencyFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

const singularUnits: Record<BillingUnit, string> = {
  "chat credits": "chat credit",
  servers: "server",
  "tool calls": "tool call",
};

export function formatBillingQuantity(
  value: number,
  unit: BillingUnit,
): string {
  const formattedUnit = value === 1 ? singularUnits[unit] : unit;
  return `${quantityFormatter.format(value)} ${formattedUnit}`;
}

export function formatBillingCurrency(value: number): string {
  return currencyFormatter.format(value);
}
