import { isValidDate } from "@/lib/trial-status";

/**
 * The parts of a Stripe subscription the plan section reads.
 *
 * Structural rather than the generated `StripeSubscription` so the derivation
 * and its tests don't move whenever the SDK regenerates: the generated status
 * is a string union, and its optional timestamps are `Date | undefined`, both
 * of which satisfy this.
 */
export type StripeSubscriptionLike = {
  status: string;
  currentPeriodStart?: Date | null | undefined;
  currentPeriodEnd?: Date | null | undefined;
  trialEnd?: Date | null | undefined;
  cancelAtPeriodEnd?: boolean | undefined;
  cancelAt?: Date | null | undefined;
  paymentFailed?: boolean | undefined;
};

/**
 * What the organization's pay-as-you-go plan is doing right now.
 *
 * - `trialing`: Stripe holds a card but hasn't started billing; `date` is when
 *   the trial converts.
 * - `active`: billing is running; `date` is when the current period renews.
 * - `ending`: a cancellation is scheduled for the end of the period; `date` is
 *   when service stops.
 * - `ended`: the subscription is over, so there is nothing to cancel or resume.
 * - `inactive`: Stripe has a subscription that never started billing
 *   (incomplete) or is paused. There is no in-product action for either; the
 *   customer portal is where they get resolved.
 */
type PaygPlanKind = "trialing" | "active" | "ending" | "ended" | "inactive";

export type PaygPlanState = {
  kind: PaygPlanKind;
  /** The date the copy for `kind` refers to, when Stripe supplied a usable one. */
  date: Date | null;
  /**
   * Whether Stripe is still trialing this subscription.
   *
   * Load-bearing for `ending`, which covers both a paid subscription winding
   * down and a trial that will never convert. Only the first one produces a
   * final invoice, so copy that promises one has to read this rather than the
   * kind alone.
   */
  trialing: boolean;
  /** Stripe reports the subscription or its latest invoice as unpaid. */
  paymentFailed: boolean;
};

/** Stripe statuses under which the organization still has service. */
const LIVE_STATUSES = new Set(["trialing", "active", "past_due"]);

/**
 * Stripe statuses under which pay-as-you-go billing is actually running.
 *
 * Narrower than {@link LIVE_STATUSES} on purpose: a trial has service but no
 * bill yet. `past_due` belongs here because the bill exists — Stripe is
 * retrying a payment against it.
 */
const BILLING_STATUSES = new Set(["active", "past_due"]);

/** Stripe statuses that mean the subscription is over for good. */
const ENDED_STATUSES = new Set(["canceled", "unpaid", "incomplete_expired"]);

/** The first usable timestamp, so a missing or malformed date falls through. */
function firstUsableDate(
  ...candidates: Array<Date | null | undefined>
): Date | null {
  return candidates.find((candidate) => isValidDate(candidate)) ?? null;
}

/**
 * Whether Stripe is still trialing this subscription.
 *
 * Read directly rather than through `paygPlanState` because a trial scheduled
 * to cancel reports as `ending` — and a trial is a trial for the purposes of
 * anything that must not act as though pay-as-you-go billing had begun.
 */
export function isStripeTrialing(
  subscription: StripeSubscriptionLike | undefined,
): boolean {
  return subscription?.status === "trialing";
}

/**
 * Whether Stripe is billing this subscription right now.
 *
 * The question anything that writes against the pay-as-you-go bill has to ask,
 * and it fails closed by construction: an absent subscription, a trial that
 * hasn't converted, and every ended or never-started status all answer false.
 * A scheduled cancellation does not — service and billing both continue until
 * the period closes.
 */
export function isStripeBilling(
  subscription: StripeSubscriptionLike | undefined,
): boolean {
  if (subscription === undefined) return false;
  return BILLING_STATUSES.has(subscription.status);
}

/**
 * Reduces a live Stripe subscription to the state the plan section renders.
 *
 * A scheduled cancellation outranks the status: an organization whose trial or
 * active subscription is set to cancel needs the end date and the way back,
 * not the lifecycle it is leaving.
 */
export function paygPlanState(
  subscription: StripeSubscriptionLike,
): PaygPlanState {
  const { status } = subscription;

  if (!LIVE_STATUSES.has(status)) {
    return {
      kind: ENDED_STATUSES.has(status) ? "ended" : "inactive",
      date: null,
      trialing: false,
      paymentFailed: false,
    };
  }

  const paymentFailed = subscription.paymentFailed === true;
  const trialing = status === "trialing";

  if (subscription.cancelAtPeriodEnd === true) {
    return {
      kind: "ending",
      // A trial that is set to cancel stops at the trial end, which is the
      // period end Stripe reports for it.
      date: firstUsableDate(
        subscription.cancelAt,
        subscription.currentPeriodEnd,
      ),
      trialing,
      paymentFailed,
    };
  }

  if (trialing) {
    return {
      kind: "trialing",
      date: firstUsableDate(
        subscription.trialEnd,
        subscription.currentPeriodEnd,
      ),
      trialing,
      paymentFailed,
    };
  }

  return {
    kind: "active",
    date: firstUsableDate(subscription.currentPeriodEnd),
    trialing,
    paymentFailed,
  };
}

/** Whether an in-product end-of-period cancellation applies to this state. */
export function canCancelPaygPlan(state: PaygPlanState): boolean {
  return state.kind === "trialing" || state.kind === "active";
}

/** Whether a scheduled cancellation can be cleared from this state. */
export function canResumePaygPlan(state: PaygPlanState): boolean {
  return state.kind === "ending";
}

// Hoisted so a formatter isn't constructed on every render.
//
// UTC, not the viewer's zone: Stripe's period and trial boundaries are UTC
// instants, and rendering one in local time moves it a day for anyone far
// enough east or west of it. "Ends on August 31" against a subscription that
// bills through September 1 is the kind of error a customer only finds after
// their access is gone.
const BILLING_DATE_FORMAT = new Intl.DateTimeFormat("en-US", {
  timeZone: "UTC",
  month: "long",
  day: "numeric",
  year: "numeric",
});

/**
 * A billing date as US English prose, or null when Stripe gave nothing usable
 * — callers fall back to copy that names no date rather than printing
 * "Invalid Date" beside a cancellation.
 */
export function formatBillingDate(
  date: Date | null | undefined,
): string | null {
  return isValidDate(date) ? BILLING_DATE_FORMAT.format(date) : null;
}
