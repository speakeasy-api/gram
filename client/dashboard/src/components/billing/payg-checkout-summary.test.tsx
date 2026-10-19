import type { StripeCheckoutSession } from "@stripe/stripe-js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import {
  PaygCheckoutHeader,
  PaygCheckoutSummary,
} from "./payg-checkout-summary";

function checkout({
  unitLabel = "tokens",
  name = "Usage",
  minorUnitsAmount = 0.000035,
  trialDays = 0,
  decimal = true,
}: {
  unitLabel?: string | null;
  name?: string;
  minorUnitsAmount?: number;
  trialDays?: number;
  decimal?: boolean;
} = {}): StripeCheckoutSession {
  // Only session fields consumed by the summary are needed for this fixture.
  return {
    currency: "usd",
    minorUnitsAmountDivisor: 100,
    recurring: {
      interval: "month",
      intervalCount: 1,
      trial: trialDays
        ? { trialEnd: Date.now() / 1000 + trialDays * 86400 }
        : null,
    },
    lineItems: [
      {
        id: "line_usage",
        name,
        unitLabel,
        unitAmount: { minorUnitsAmount: decimal ? 0 : minorUnitsAmount },
        unitAmountDecimal: decimal ? { minorUnitsAmount } : null,
        recurring: { usageType: "metered" },
      },
    ],
    total: { total: { amount: "$0.00" } },
  } as StripeCheckoutSession;
}

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

it("uses the pricing heading and actual Stripe trial duration", () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
  const session = checkout({ trialDays: 9 });
  render(
    <>
      <PaygCheckoutHeader checkout={session} />
      <PaygCheckoutSummary checkout={session} />
    </>,
  );
  expect(
    screen.getByRole("heading", { name: "Pay as you go pricing" }),
  ).toBeTruthy();
  expect(
    screen.getByText(
      "9 days during a trial, billing starts when the trial converts.",
    ),
  ).toBeTruthy();
  expect(
    screen.getByText("Then $0.35 per M tokens, billed based on usage"),
  ).toBeTruthy();
});

it.each([
  ["token", 0.000035, "$0.35"],
  ["tokens", 0.000099, "$0.99"],
  ["tokens", 0.0000350000123, "$0.350000123"],
  ["tokens", 1e-8, "$0.0001"],
])(
  "scales %s decimal amount %s without rounding",
  (unitLabel, minorUnitsAmount, expected) => {
    render(
      <PaygCheckoutSummary
        checkout={checkout({ unitLabel, minorUnitsAmount })}
      />,
    );
    expect(
      screen.getByText(`${expected} per M tokens, billed based on usage`),
    ).toBeTruthy();
  },
);

it.each([
  ["GiB", 2000, "$20.00 per GiB"],
  ["request", 0.000035, "$0.00000035 per request"],
  [null, 0.000035, "$0.00000035 per unit"],
  ["million tokens", 35, "$0.35 per million tokens"],
])("does not rescale the unit %s", (unitLabel, minorUnitsAmount, expected) => {
  render(
    <PaygCheckoutSummary
      checkout={checkout({ unitLabel, minorUnitsAmount })}
    />,
  );
  expect(screen.getByText(`${expected}, billed based on usage`)).toBeTruthy();
});

it("falls back to the non-decimal unit amount", () => {
  render(
    <PaygCheckoutSummary
      checkout={checkout({
        unitLabel: "GiB",
        minorUnitsAmount: 2000,
        decimal: false,
      })}
    />,
  );
  expect(
    screen.getByText("$20.00 per GiB, billed based on usage"),
  ).toBeTruthy();
});

it("does not advertise a trial when Stripe has none", () => {
  const session = checkout();
  render(
    <>
      <PaygCheckoutHeader checkout={session} />
      <PaygCheckoutSummary checkout={session} />
    </>,
  );
  expect(
    screen.getByRole("heading", { name: "Pay as you go pricing" }),
  ).toBeTruthy();
  expect(screen.getByText("Billed based on usage each month.")).toBeTruthy();
  expect(
    screen.queryByText(/days free|during a trial|Total after trial/),
  ).toBeNull();
});

it.each([
  ["Tokens under management", 0.000035, "$0.35 per M tokens"],
  ["Sandbox MCP egress", 0.000001862645, "$20.00 per GiB"],
  ["Sandbox risk scans", 0.000099, "$0.99 per M tokens scanned"],
  ["AI Control Plane PAYG", 0.000035, "$0.35 per M tokens"],
  ["Unknown tokens", 0.000035, "$0.00000035 per unit"],
  ["Unknown egress", 0.000001862645, "$0.00000001862645 per unit"],
])("handles unlabeled catalog item %s", (name, minorUnitsAmount, expected) => {
  render(
    <PaygCheckoutSummary
      checkout={checkout({ name, unitLabel: null, minorUnitsAmount })}
    />,
  );
  expect(screen.getByText(`${expected}, billed based on usage`)).toBeTruthy();
});

it.each([
  ["Sandbox MCP egress", "GiB", 2000, "$20.00 per GiB"],
  ["Tokens under management", "request", 35, "$0.35 per request"],
  ["Sandbox risk scans", "million tokens", 99, "$0.99 per million tokens"],
  ["Usage", "bytes", 0.000001862645, "$20.00 per GiB"],
  ["Sandbox risk scans", null, 0.00000001, "$0.0001 per M tokens scanned"],
  ["Sandbox MCP egress", null, 0.000002, "$21.47 per GiB"],
])(
  "uses Stripe's rate and explicit units for %s (%s)",
  (name, unitLabel, minorUnitsAmount, expected) => {
    render(
      <PaygCheckoutSummary
        checkout={checkout({ name, unitLabel, minorUnitsAmount })}
      />,
    );
    expect(screen.getByText(`${expected}, billed based on usage`)).toBeTruthy();
  },
);
