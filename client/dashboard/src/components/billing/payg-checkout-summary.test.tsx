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
  interval = "month",
  intervalCount = 1,
  metered = true,
  dueToday = "$0.00",
  dueNext = "$49.00",
  description = null,
}: {
  unitLabel?: string | null;
  name?: string;
  minorUnitsAmount?: number;
  trialDays?: number;
  decimal?: boolean;
  interval?: "month" | "year" | "week";
  intervalCount?: number;
  metered?: boolean;
  dueToday?: string;
  dueNext?: string;
  description?: string | null;
} = {}): StripeCheckoutSession {
  // Only session fields consumed by the summary are needed for this fixture.
  return {
    currency: "usd",
    minorUnitsAmountDivisor: 100,
    recurring: {
      interval,
      intervalCount,
      dueNext: { total: { amount: dueNext } },
      trial: trialDays
        ? { trialEnd: Date.now() / 1000 + trialDays * 86400 }
        : null,
    },
    lineItems: [
      {
        id: "line_usage",
        name,
        description,
        unitLabel,
        unitAmount: { minorUnitsAmount: decimal ? 0 : minorUnitsAmount },
        unitAmountDecimal: decimal ? { minorUnitsAmount } : null,
        recurring: { usageType: metered ? "metered" : "licensed" },
      },
    ],
    total: { total: { amount: dueToday } },
  } as StripeCheckoutSession;
}

function expectRate(expected: string): void {
  expect(
    screen.getByText(
      (_, element) =>
        element?.tagName === "P" && element.textContent === expected,
    ),
  ).toBeTruthy();
}

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

it("uses the small pricing heading without repeating trial copy", () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
  const session = checkout({ trialDays: 9 });
  render(
    <>
      <PaygCheckoutHeader />
      <PaygCheckoutSummary checkout={session} />
    </>,
  );
  expect(
    screen.getByRole("heading", { name: "Pricing", level: 4 }),
  ).toBeTruthy();
  expect(
    screen.queryByRole("heading", { name: "Pay as you go pricing" }),
  ).toBeNull();
  expect(
    screen
      .getByRole("heading", { name: "Pricing" })
      .classList.contains("text-display-sm"),
  ).toBe(false);
  expect(
    screen.queryByText("No charge today. Billing starts when your trial ends."),
  ).toBeNull();
  expectRate("$0.35 per million tokens");
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
    expectRate(`${expected} per million tokens`);
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
  expectRate(expected);
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
  expectRate("$20.00 per GiB");
});

it("does not advertise a trial when Stripe has none", () => {
  const session = checkout();
  render(
    <>
      <PaygCheckoutHeader />
      <PaygCheckoutSummary checkout={session} />
    </>,
  );
  expect(
    screen.getByRole("heading", { name: "Pricing", level: 4 }),
  ).toBeTruthy();
  expect(screen.queryByText("Billed based on usage each month.")).toBeNull();
  expect(
    screen.queryByText(/days free|during a trial|Total after trial/),
  ).toBeNull();
});

it.each([
  ["Tokens under management", 0.000035, "$0.35 per million tokens"],
  ["Sandbox MCP egress", 0.000001862645, "$20.00 per GiB of egress"],
  ["Sandbox risk scans", 0.000099, "$0.99 per million tokens scanned"],
  ["AI Control Plane PAYG", 0.000035, "$0.35 per million tokens"],
  ["Unknown tokens", 0.000035, "$0.00000035 per unit"],
  ["Unknown egress", 0.000001862645, "$0.00000001862645 per unit"],
])("handles unlabeled catalog item %s", (name, minorUnitsAmount, expected) => {
  render(
    <PaygCheckoutSummary
      checkout={checkout({ name, unitLabel: null, minorUnitsAmount })}
    />,
  );
  expectRate(expected);
});

it.each([
  ["Sandbox MCP egress", "GiB", 2000, "$20.00 per GiB"],
  ["Tokens under management", "request", 35, "$0.35 per request"],
  ["Sandbox risk scans", "million tokens", 99, "$0.99 per million tokens"],
  ["Usage", "bytes", 0.000001862645, "$20.00 per GiB of egress"],
  [
    "Sandbox risk scans",
    null,
    0.00000001,
    "$0.0001 per million tokens scanned",
  ],
  ["Sandbox MCP egress", null, 0.000002, "$21.47 per GiB of egress"],
  ["Sandbox MCP egress", null, 1e-12, "$0.00001073741824 per GiB of egress"],
  ["Usage", "bytes", 1e-10, "$0.001073741824 per GiB of egress"],
  ["Usage", "bytes", 0, "$0.00 per GiB of egress"],
  ["Usage", "tokens", 0, "$0.00 per million tokens"],
  ["Usage", "request", 0, "$0.00 per request"],
])(
  "uses Stripe's rate and explicit units for %s (%s)",
  (name, unitLabel, minorUnitsAmount, expected) => {
    render(
      <PaygCheckoutSummary
        checkout={checkout({ name, unitLabel, minorUnitsAmount })}
      />,
    );
    expectRate(expected);
  },
);

it.each([
  ["AI Control Plane PAYG", "AI Control Plane PAYG"],
  ["Tokens under management", "Tokens under management"],
  ["Sandbox risk scans", "Sandbox risk scans"],
  ["Sandbox MCP egress", "Sandbox MCP egress"],
  ["Unknown tokens", "Unknown tokens"],
  ["Sandbox risk scans custom", "Sandbox risk scans custom"],
])("preserves the exact Stripe product name %s", (name, label) => {
  render(<PaygCheckoutSummary checkout={checkout({ name })} />);
  expect(screen.getByRole("heading", { name: label })).toBeTruthy();
});

it("shows trial billing once and the actual amount due today", () => {
  const session = checkout({ trialDays: 9, dueToday: "$12.34" });
  session.lineItems.push({ ...session.lineItems[0]!, id: "other" });
  render(<PaygCheckoutSummary checkout={session} />);
  expect(screen.getByText(/^Trial ends /)).toBeTruthy();
  expect(screen.getAllByText("Billed monthly based on usage")).toHaveLength(1);
  expect(screen.getAllByText("$12.34")).toHaveLength(2);
  expect(
    screen.queryByText(/days free|Then |, billed based on usage/),
  ).toBeNull();
});

it("uses Billing for monthly metered checkout without a trial", () => {
  render(<PaygCheckoutSummary checkout={checkout()} />);
  expect(screen.getByText("Billing")).toBeTruthy();
  expect(screen.getByText("Billed monthly based on usage")).toBeTruthy();
  expect(screen.queryByText("After your trial")).toBeNull();
});

it.each([
  ["year", 1],
  ["week", 1],
  ["month", 3],
] as const)(
  "does not claim monthly billing for %s count %s",
  (interval, intervalCount) => {
    render(
      <PaygCheckoutSummary checkout={checkout({ interval, intervalCount })} />,
    );
    expect(screen.getByText("Billed based on usage")).toBeTruthy();
    expect(screen.queryByText(/monthly/)).toBeNull();
  },
);

it("preserves nonmetered trial totals from Stripe", () => {
  render(
    <PaygCheckoutSummary
      checkout={checkout({
        metered: false,
        trialDays: 9,
        dueToday: "$12.34",
        dueNext: "$56.78",
      })}
    />,
  );
  expect(screen.getByText(/^Trial ends /)).toBeTruthy();
  expect(screen.getByText("$56.78")).toBeTruthy();
  expect(screen.getAllByText("$12.34")).toHaveLength(2);
  expect(screen.queryByText(/Billed.*usage/)).toBeNull();
});

it("preserves nontrial nonmetered totals from Stripe", () => {
  render(
    <PaygCheckoutSummary
      checkout={checkout({ metered: false, dueToday: "$23.45" })}
    />,
  );
  expect(screen.getByText("Total")).toBeTruthy();
  expect(screen.getByText("$49.00")).toBeTruthy();
  expect(screen.getAllByText("$23.45")).toHaveLength(2);
  expect(screen.queryByText(/Billed.*usage/)).toBeNull();
});

it.each([null, "", "   "])(
  "does not duplicate the rate below the title when description is %s",
  (description) => {
    render(<PaygCheckoutSummary checkout={checkout({ description })} />);
    const heading = screen.getByRole("heading", { name: "Usage" });
    expect(heading.nextElementSibling).toBeNull();
  },
);

it.each([
  "Includes retained session context.",
  "$0.35 / M tokens",
  "$0.35 per M tokens",
  "Usage",
])(
  "keeps the compact product row independent of Stripe description: %s",
  (description) => {
    render(<PaygCheckoutSummary checkout={checkout({ description })} />);
    const item = screen.getByRole("listitem");
    const heading = screen.getByRole("heading", { name: "Usage" });
    expect(heading.nextElementSibling).toBeNull();
    expect(item.querySelectorAll("p")).toHaveLength(1);
    expectRate("$0.35 per million tokens");
    expect(item.textContent).toBe("Usage$0.35 per million tokens");
  },
);

it("shows explanatory timing blocks before a separate due today total", () => {
  const { container } = render(
    <PaygCheckoutSummary
      checkout={checkout({ trialDays: 9, dueToday: "$12.34" })}
    />,
  );
  expect(
    Array.from(container.querySelectorAll("dt"), (node) => node.textContent),
  ).toEqual(["Today", expect.stringMatching(/^Trial ends /), "Due today"]);
  expect(
    screen.queryByText(/What happens next|Trial starts|After day/),
  ).toBeNull();
  expect(screen.getByText("Billed monthly based on usage")).toBeTruthy();
  expect(screen.getAllByText("$12.34")).toHaveLength(2);
});

it("uses the actual trial end, not a new fixed-length trial", () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
  const session = checkout({ trialDays: 9 });
  const { container } = render(<PaygCheckoutSummary checkout={session} />);
  const date = new Intl.DateTimeFormat(undefined, { dateStyle: "long" }).format(
    new Date(session.recurring!.trial!.trialEnd * 1000),
  );
  expect(screen.getByText(`Trial ends ${date}`).tagName).toBe("DT");
  expect(
    screen.getByText("Trial active").closest("dd")?.previousElementSibling
      ?.textContent,
  ).toBe("Today");
  expect(
    screen.getByText("Billed monthly based on usage").closest("dd")
      ?.previousElementSibling?.textContent,
  ).toBe(`Trial ends ${date}`);
  expect(
    screen.queryByText(/Trial starts|14 days|What happens next/),
  ).toBeNull();
  expect(container.querySelectorAll("dl")).toHaveLength(2);
  expect(screen.getByText("Due today").closest("dl")?.className).toContain(
    "border-border",
  );
});

it("does not describe an expired trial as active", () => {
  render(<PaygCheckoutSummary checkout={checkout({ trialDays: -1 })} />);
  expect(screen.getByText("Payment due today")).toBeTruthy();
  expect(screen.queryByText(/Trial ends|After your trial/)).toBeNull();
});

it("aligns timeline amounts on the right, separately from the final total", () => {
  render(
    <PaygCheckoutSummary
      checkout={checkout({ trialDays: 9, dueToday: "$12.34" })}
    />,
  );
  const [today, total] = screen.getAllByText("$12.34");
  expect(today?.className).toContain("text-right");
  expect(today?.closest("dd")?.className).toContain("items-baseline");
  expect(today?.closest("dd")?.className).toContain("justify-between");
  expect(total?.closest("dl")).not.toBe(today?.closest("dl"));
  expect(total?.className).toContain("text-xl");
  const usage = screen.getByText("Usage-based");
  expect(usage.className).toContain("text-right");
  expect(usage.closest("dd")?.className).toContain("items-baseline");
  expect(usage.closest("dd")?.textContent).toContain(
    "Billed monthly based on usage",
  );
  expect(screen.queryByText("$0.00")).toBeNull();
  expect(screen.queryByText(/14 days|After day 14/)).toBeNull();
});
