import { describe, expect, it, vi } from "vitest";
import {
  canCancelPaygPlan,
  canResumePaygPlan,
  formatBillingDate,
  isStripeBilling,
  isStripeTrialing,
  paygPlanState,
  type StripeSubscriptionLike,
} from "./payg-plan-state";

// Midday UTC so the formatted day can't slide either side of the date line in
// whichever time zone the tests happen to run in.
const PERIOD_END = new Date("2026-09-01T12:00:00.000Z");
const TRIAL_END = new Date("2026-08-20T12:00:00.000Z");
const CANCEL_AT = new Date("2026-08-25T12:00:00.000Z");

function subscription(
  overrides: Partial<StripeSubscriptionLike> = {},
): StripeSubscriptionLike {
  return {
    status: "active",
    currentPeriodEnd: PERIOD_END,
    cancelAtPeriodEnd: false,
    paymentFailed: false,
    ...overrides,
  };
}

describe("paygPlanState", () => {
  it("reads an active subscription off the current period", () => {
    const state = paygPlanState(subscription());

    expect(state.kind).toBe("active");
    expect(state.date).toBe(PERIOD_END);
    expect(canCancelPaygPlan(state)).toBe(true);
    expect(canResumePaygPlan(state)).toBe(false);
  });

  // A failed payment doesn't stop service — Stripe keeps retrying — so the
  // organization stays on the active plan with the failure flagged.
  it("keeps a past due subscription active and flags the failed payment", () => {
    const state = paygPlanState(
      subscription({ status: "past_due", paymentFailed: true }),
    );

    expect(state.kind).toBe("active");
    expect(state.paymentFailed).toBe(true);
    expect(canCancelPaygPlan(state)).toBe(true);
  });

  it("dates a trial from the trial end", () => {
    const state = paygPlanState(
      subscription({ status: "trialing", trialEnd: TRIAL_END }),
    );

    expect(state.kind).toBe("trialing");
    expect(state.date).toBe(TRIAL_END);
    // A trial can be ended early, so cancellation is still on offer.
    expect(canCancelPaygPlan(state)).toBe(true);
  });

  // Stripe only sends a trial end when one is configured, and the period end
  // is the date the customer would otherwise be looking for.
  it.each([
    ["no trial end", undefined],
    ["an unusable trial end", new Date("nonsense")],
  ])("falls back to the period end given %s", (_name, trialEnd) => {
    const state = paygPlanState(
      subscription({ status: "trialing", trialEnd: trialEnd as Date }),
    );

    expect(state.kind).toBe("trialing");
    expect(state.date).toBe(PERIOD_END);
  });

  it("dates a scheduled cancellation from the cancellation time", () => {
    const state = paygPlanState(
      subscription({ cancelAtPeriodEnd: true, cancelAt: CANCEL_AT }),
    );

    expect(state.kind).toBe("ending");
    expect(state.date).toBe(CANCEL_AT);
    expect(canResumePaygPlan(state)).toBe(true);
    // Canceling twice is meaningless; the way out is resuming.
    expect(canCancelPaygPlan(state)).toBe(false);
  });

  it("falls back to the period end when no cancellation time is set", () => {
    const state = paygPlanState(subscription({ cancelAtPeriodEnd: true }));

    expect(state.kind).toBe("ending");
    expect(state.date).toBe(PERIOD_END);
  });

  // The organization needs the date service stops and the way back, not the
  // lifecycle it happens to be leaving.
  it("puts a trial that is set to cancel into the ending state", () => {
    const state = paygPlanState(
      subscription({
        status: "trialing",
        trialEnd: TRIAL_END,
        cancelAtPeriodEnd: true,
        cancelAt: CANCEL_AT,
      }),
    );

    expect(state.kind).toBe("ending");
    expect(state.date).toBe(CANCEL_AT);
  });

  // `ending` covers a paid subscription winding down and a trial that will
  // never convert. Only the first produces a final invoice, so the state has to
  // carry the difference — copy can't read it off the kind.
  it("marks an ending trial as trialing and an ending subscription as not", () => {
    const endingTrial = paygPlanState(
      subscription({ status: "trialing", cancelAtPeriodEnd: true }),
    );
    const endingPaid = paygPlanState(
      subscription({ status: "active", cancelAtPeriodEnd: true }),
    );

    expect(endingTrial.kind).toBe("ending");
    expect(endingTrial.trialing).toBe(true);
    expect(endingPaid.kind).toBe("ending");
    expect(endingPaid.trialing).toBe(false);
  });

  it.each([
    ["trialing", true],
    ["active", false],
    ["past_due", false],
    ["canceled", false],
    ["paused", false],
  ] as Array<[string, boolean]>)(
    "carries the trialing flag for a %s subscription",
    (status, trialing) => {
      expect(paygPlanState(subscription({ status })).trialing).toBe(trialing);
    },
  );

  it.each(["canceled", "unpaid", "incomplete_expired"])(
    "reports %s as ended, with nothing left to act on",
    (status) => {
      const state = paygPlanState(subscription({ status }));

      expect(state.kind).toBe("ended");
      expect(state.date).toBeNull();
      expect(canCancelPaygPlan(state)).toBe(false);
      expect(canResumePaygPlan(state)).toBe(false);
    },
  );

  it.each(["incomplete", "paused"])(
    "reports %s as inactive, with nothing left to act on",
    (status) => {
      const state = paygPlanState(subscription({ status }));

      expect(state.kind).toBe("inactive");
      expect(canCancelPaygPlan(state)).toBe(false);
      expect(canResumePaygPlan(state)).toBe(false);
    },
  );

  // A subscription that is over can't have a payment outstanding to warn about.
  it("drops the payment failure once the subscription is over", () => {
    const state = paygPlanState(
      subscription({ status: "canceled", paymentFailed: true }),
    );

    expect(state.paymentFailed).toBe(false);
  });
});

describe("isStripeTrialing", () => {
  it("is true while Stripe is trialing", () => {
    expect(isStripeTrialing(subscription({ status: "trialing" }))).toBe(true);
  });

  // Anything gating on "is this organization being billed yet" has to treat a
  // trial as a trial even when it is on its way out.
  it("stays true for a trial that is set to cancel", () => {
    expect(
      isStripeTrialing(
        subscription({ status: "trialing", cancelAtPeriodEnd: true }),
      ),
    ).toBe(true);
  });

  it.each(["active", "past_due", "canceled"])("is false for %s", (status) => {
    expect(isStripeTrialing(subscription({ status }))).toBe(false);
  });

  it("is false when there is no subscription to read", () => {
    expect(isStripeTrialing(undefined)).toBe(false);
  });
});

describe("isStripeBilling", () => {
  // The question anything writing against the pay-as-you-go bill has to ask.
  it.each(["active", "past_due"])("is true for %s", (status) => {
    expect(isStripeBilling(subscription({ status }))).toBe(true);
  });

  // Service and billing both run to the end of the period.
  it("stays true through a scheduled cancellation", () => {
    expect(isStripeBilling(subscription({ cancelAtPeriodEnd: true }))).toBe(
      true,
    );
  });

  // A trial has service but no bill yet, and the rest never had one or lost it.
  it.each([
    "trialing",
    "canceled",
    "unpaid",
    "incomplete",
    "incomplete_expired",
    "paused",
  ])("is false for %s", (status) => {
    expect(isStripeBilling(subscription({ status }))).toBe(false);
  });

  it("is false when there is no subscription to read", () => {
    expect(isStripeBilling(undefined)).toBe(false);
  });
});

/**
 * `formatBillingDate` as it behaves for a viewer whose environment sits in
 * `timeZone`, whichever zone the test runner itself is in.
 *
 * There is no way to ask an `Intl.DateTimeFormat` to behave as though it were
 * built somewhere else, so the zone goes in underneath: every formatter
 * constructed while the stub is up defaults to `timeZone` unless its own
 * options pin one — which is exactly the pin under test. The module builds its
 * formatter once, at import time, so it has to be re-imported under the stub
 * rather than merely called under it.
 */
async function formatBillingDateInZone(
  timeZone: string,
): Promise<typeof formatBillingDate> {
  const RealDateTimeFormat = Intl.DateTimeFormat;

  // A `function` rather than an arrow: the module builds its formatter with
  // `new`, which an arrow can't answer. What comes back is a real
  // `Intl.DateTimeFormat`, which is what `new` on a function yields.
  function ZonedDateTimeFormat(
    locales?: Intl.LocalesArgument,
    options?: Intl.DateTimeFormatOptions,
  ): Intl.DateTimeFormat {
    return new RealDateTimeFormat(locales, { timeZone, ...options });
  }

  Intl.DateTimeFormat =
    ZonedDateTimeFormat as unknown as typeof Intl.DateTimeFormat;

  try {
    vi.resetModules();
    const { formatBillingDate: formatInZone } =
      await import("./payg-plan-state");
    return formatInZone;
  } finally {
    // The formatter is already built, so the stub has done its work and the
    // rest of the suite gets the real constructor back.
    Intl.DateTimeFormat = RealDateTimeFormat;
    vi.resetModules();
  }
}

describe("formatBillingDate", () => {
  it("writes the date out in US English", () => {
    expect(formatBillingDate(PERIOD_END)).toBe("September 1, 2026");
  });

  // Stripe's period and trial boundaries are UTC instants. Formatted in the
  // viewer's zone they move a day for anyone far enough east or west — "ends on
  // August 31" against a subscription that bills through September 1. These two
  // instants share a UTC day but fall on different local days in every non-zero
  // offset, so they render identically only if the formatter is pinned to UTC.
  //
  // The zone is forced rather than inherited: in a UTC runner every instant
  // renders the same either way, so dropping the pin would be invisible and
  // this test would be guarding nothing on the machine it runs on.
  it.each(["America/Los_Angeles", "Asia/Tokyo"])(
    "renders a UTC instant in UTC for a viewer in %s",
    async (timeZone) => {
      const justAfterMidnight = new Date("2026-09-01T00:30:00.000Z");
      const justBeforeMidnight = new Date("2026-09-01T23:30:00.000Z");
      // West of UTC the first instant is the day before; east of it the second
      // is the day after. Either way an unpinned formatter splits the pair.
      const formatForViewer = await formatBillingDateInZone(timeZone);

      expect(formatForViewer(justAfterMidnight)).toBe("September 1, 2026");
      expect(formatForViewer(justBeforeMidnight)).toBe("September 1, 2026");
    },
  );

  // The boundary Stripe actually sends for a period end: exact midnight UTC.
  it("keeps a midnight UTC boundary on its own day", () => {
    expect(formatBillingDate(new Date("2026-09-01T00:00:00.000Z"))).toBe(
      "September 1, 2026",
    );
  });

  // Copy falls back to a sentence that names no date rather than printing
  // "Invalid Date" beside a cancellation.
  it.each([
    ["nothing", null],
    ["an absent date", undefined],
    ["an unusable date", new Date("nonsense")],
  ])("returns null for %s", (_name, value) => {
    expect(formatBillingDate(value)).toBeNull();
  });
});
