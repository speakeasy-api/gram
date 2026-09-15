import type { AdminStripeSubscription } from "@/lib/gramAdminApi";

export type BillingState = {
  kind: "trialing" | "active" | "ending" | "ended" | "inactive";
  date: Date | null;
  paymentFailed: boolean;
};

const LIVE_STATUSES = new Set(["trialing", "active", "past_due"]);
const ENDED_STATUSES = new Set(["canceled", "unpaid", "incomplete_expired"]);
const EXACT_DECIMAL = /^([+-]?)(\d+)(?:\.(\d*))?$/;
const RECORDED_THROUGH = /^(\d{4})-(\d{2})-(\d{2})$/;

const BILLING_DATE_FORMAT = new Intl.DateTimeFormat("en-US", {
  timeZone: "UTC",
  month: "long",
  day: "numeric",
  year: "numeric",
});

function usableDate(value: string | undefined): Date | null {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

export function billingState(
  subscription: AdminStripeSubscription,
): BillingState {
  if (!LIVE_STATUSES.has(subscription.status)) {
    return {
      kind: ENDED_STATUSES.has(subscription.status) ? "ended" : "inactive",
      date: null,
      paymentFailed: false,
    };
  }

  const paymentFailed = subscription.payment_failed;
  if (subscription.cancel_at_period_end) {
    return {
      kind: "ending",
      date:
        usableDate(subscription.cancel_at) ??
        usableDate(subscription.current_period_end),
      paymentFailed,
    };
  }
  if (subscription.status === "trialing") {
    return {
      kind: "trialing",
      date:
        usableDate(subscription.trial_end) ??
        usableDate(subscription.current_period_end),
      paymentFailed,
    };
  }
  return {
    kind: "active",
    date: usableDate(subscription.current_period_end),
    paymentFailed,
  };
}

export function formatBillingDate(
  value: string | Date | null | undefined,
): string | null {
  const date = value instanceof Date ? value : usableDate(value ?? undefined);
  return date === null || Number.isNaN(date.getTime())
    ? null
    : BILLING_DATE_FORMAT.format(date);
}

function groupDigits(digits: string): string {
  return digits.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

export function formatExactUsd(amount: string | undefined): string | null {
  if (amount === undefined) return null;
  const match = EXACT_DECIMAL.exec(amount.trim());
  if (match === null) return null;

  const [, sign = "", whole = "", fraction = ""] = match;
  const cents = fraction.replace(/0+$/, "").padEnd(2, "0");
  const dollars = groupDigits(whole.replace(/^0+(?=\d)/, ""));
  const zero = /^0*$/.test(whole) && /^0*$/.test(fraction);
  return `${sign === "-" && !zero ? "-" : ""}$${dollars}.${cents}`;
}

export function formatTokenCount(tokens: number): string | null {
  if (!Number.isSafeInteger(tokens)) return null;
  return groupDigits(String(tokens));
}

export function formatRecordedThrough(
  value: string | undefined,
): string | null {
  if (!value) return null;
  const match = RECORDED_THROUGH.exec(value);
  if (match === null) return null;
  const [, year = "", month = "", day = ""] = match;
  const date = new Date(
    Date.UTC(Number(year), Number(month) - 1, Number(day), 12),
  );
  if (date.getUTCFullYear() !== Number(year)) return null;
  if (date.getUTCMonth() !== Number(month) - 1) return null;
  if (date.getUTCDate() !== Number(day)) return null;
  return formatBillingDate(date);
}
