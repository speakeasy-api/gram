import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render as rtlRender,
  screen,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { StripeSubscription } from "@gram/client/models/components/stripesubscription.js";
import type { ProductTier } from "@/hooks/useProductTier";

const mocks = vi.hoisted(() => ({
  productTier: vi.fn(),
  session: vi.fn(),
  query: vi.fn(),
  refetch: vi.fn(),
  hasAnyScope: vi.fn(),
  portalMutate: vi.fn(),
  cancelMutate: vi.fn(),
  resumeMutate: vi.fn(),
  invalidate: vi.fn(),
}));

vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => mocks.productTier() as ProductTier,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: Scope) => mocks.hasAnyScope([scope]) as boolean,
    hasAnyScope: (scopes: Scope[]) => mocks.hasAnyScope(scopes) as boolean,
    hasAllScopes: (scopes: Scope[]) => mocks.hasAnyScope(scopes) as boolean,
    isLoading: false,
  }),
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => mocks.session(),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: vi.fn() }),
}));

vi.mock("@gram/client/react-query/getStripeSubscription.js", () => ({
  // The hook options are part of what's under test — the read has to opt out
  // of the shared throwOnError — so the arguments are forwarded, not dropped.
  useGetStripeSubscription: (...args: unknown[]) => mocks.query(...args),
  invalidateAllGetStripeSubscription: mocks.invalidate,
}));

vi.mock("@gram/client/react-query/createStripePortalSession.js", () => ({
  useCreateStripePortalSessionMutation: () => ({
    mutate: mocks.portalMutate,
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/cancelStripeSubscription.js", () => ({
  useCancelStripeSubscriptionMutation: () => ({
    mutate: mocks.cancelMutate,
    reset: vi.fn(),
    isPending: false,
    isError: false,
  }),
}));

vi.mock("@gram/client/react-query/resumeStripeSubscription.js", () => ({
  useResumeStripeSubscriptionMutation: () => ({
    mutate: mocks.resumeMutate,
    isPending: false,
    isError: false,
  }),
}));

// Page chrome isn't what's under test; render it as plain boxes so a failure
// here can only mean the section.
vi.mock("@/components/page-layout", () => {
  const Section = ({ children }: { children: ReactNode }) => <>{children}</>;
  Section.Title = ({ children }: { children: ReactNode }) => (
    <h2>{children}</h2>
  );
  Section.Description = ({ children }: { children: ReactNode }) => (
    <p>{children}</p>
  );
  Section.Body = ({ children }: { children: ReactNode }) => <>{children}</>;
  return { Page: { Section } };
});

import { PaygPlanSection } from "./payg-plan-section";

// Midday UTC so the formatted day can't slide either side of the date line in
// whichever time zone the tests happen to run in.
const PERIOD_START = new Date("2026-08-01T12:00:00.000Z");
const PERIOD_END = new Date("2026-09-01T12:00:00.000Z");
const TRIAL_END = new Date("2026-08-20T12:00:00.000Z");
const CANCEL_AT = new Date("2026-08-25T12:00:00.000Z");

function subscription(
  overrides: Partial<StripeSubscription> = {},
): StripeSubscription {
  return {
    status: "active",
    currentPeriodStart: PERIOD_START,
    currentPeriodEnd: PERIOD_END,
    cancelAtPeriodEnd: false,
    paymentFailed: false,
    ...overrides,
  };
}

type QueryState = {
  data?: StripeSubscription | undefined;
  error?: unknown;
  isError?: boolean;
  isFetching?: boolean;
};

function queryState({
  data,
  error,
  isError = false,
  isFetching = false,
}: QueryState = {}) {
  mocks.query.mockReturnValue({
    data,
    error,
    isError,
    isFetching,
    refetch: mocks.refetch,
  });
}

/** Shaped like the SDK's 404 rejection, which is what the branch keys on. */
function notFound(): Error {
  return Object.assign(new Error("subscription not found"), {
    statusCode: 404,
  });
}

/** The section's children reach for the query client to invalidate. */
function render(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return rtlRender(
    <QueryClientProvider client={client}>{ui}</QueryClientProvider>,
  );
}

const heading = () => screen.queryByRole("heading", { name: /^plan$/i });

const portalButton = () =>
  screen.queryByRole("button", { name: /manage billing/i });

const cancelTrigger = () =>
  screen.queryByRole("button", { name: /^cancel pay as you go$/i });

const resumeButton = () =>
  screen.queryByRole("button", { name: /^resume pay as you go$/i });

const DAY = 24 * 60 * 60 * 1000;

describe("PaygPlanSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.productTier.mockReturnValue("payg");
    mocks.hasAnyScope.mockReturnValue(true);
    mocks.session.mockReturnValue({ trial: null });
    queryState({ data: subscription() });
  });

  afterEach(cleanup);

  describe("while Stripe is trialing", () => {
    beforeEach(() => {
      queryState({
        data: subscription({ status: "trialing", trialEnd: TRIAL_END }),
      });
    });

    it("says when the trial converts to pay as you go", () => {
      render(<PaygPlanSection />);

      expect(
        screen.getByText(
          /trial — converts to pay as you go on August 20, 2026/i,
        ),
      ).toBeTruthy();
    });

    // A trial can be ended before it converts, so the way out is still offered.
    it("offers cancellation and no resume", () => {
      render(<PaygPlanSection />);

      expect(cancelTrigger()).not.toBeNull();
      expect(resumeButton()).toBeNull();
    });

    // Canceling a trial stops it converting, so no period is ever billed. The
    // confirmation has to be told which lifecycle it is ending.
    it("confirms cancellation without promising an invoice", () => {
      render(<PaygPlanSection />);

      fireEvent.click(cancelTrigger()!);

      const description = screen.getByRole("dialog").textContent ?? "";
      expect(description).toMatch(/pay as you go never starts/i);
      expect(description).not.toMatch(/final invoice/i);
    });
  });

  describe("while pay as you go is active", () => {
    it("names the plan and when the period ends", () => {
      render(<PaygPlanSection />);

      expect(screen.getByText("Pay as you go")).toBeTruthy();
      expect(
        screen.getByText(/current period ends on September 1, 2026/i),
      ).toBeTruthy();
      expect(cancelTrigger()).not.toBeNull();
      expect(resumeButton()).toBeNull();
    });

    // Stripe keeps retrying a failed payment, so service continues and the plan
    // is still the plan. Saying so a second time here, below the fold, would
    // only compete with the banner heading the page.
    it("leaves a failed payment to the banner", () => {
      queryState({
        data: subscription({ status: "past_due", paymentFailed: true }),
      });

      render(<PaygPlanSection />);

      expect(screen.getByText("Pay as you go")).toBeTruthy();
      expect(screen.queryByText(/last payment failed/i)).toBeNull();
      expect(cancelTrigger()).not.toBeNull();
    });
  });

  describe("once a cancellation is scheduled", () => {
    beforeEach(() => {
      queryState({
        data: subscription({ cancelAtPeriodEnd: true, cancelAt: CANCEL_AT }),
      });
    });

    it("says when pay as you go ends", () => {
      render(<PaygPlanSection />);

      expect(
        screen.getByText(/pay as you go — ends on August 25, 2026/i),
      ).toBeTruthy();
    });

    // The period already started, so its usage is billed after it closes.
    it("promises the final invoice a paid period still produces", () => {
      render(<PaygPlanSection />);

      expect(screen.getByText(/a final invoice follows/i)).toBeTruthy();
    });

    // Canceling twice is meaningless; resuming is the only move left.
    it("offers resume in place of cancellation", () => {
      render(<PaygPlanSection />);

      expect(resumeButton()).not.toBeNull();
      expect(cancelTrigger()).toBeNull();
    });

    it("resumes on click", () => {
      render(<PaygPlanSection />);

      fireEvent.click(resumeButton()!);

      expect(mocks.resumeMutate).toHaveBeenCalledTimes(1);
    });
  });

  // A trial set to cancel never converts, so no pay-as-you-go period is ever
  // billed. Reusing the paid copy would have the customer waiting on a charge
  // that isn't coming.
  describe("once a trial is scheduled to cancel", () => {
    beforeEach(() => {
      queryState({
        data: subscription({
          status: "trialing",
          trialEnd: TRIAL_END,
          cancelAtPeriodEnd: true,
          cancelAt: TRIAL_END,
        }),
      });
    });

    it("says the trial ends rather than pay as you go", () => {
      render(<PaygPlanSection />);

      expect(
        screen.getByText(/^trial — ends on August 20, 2026$/i),
      ).toBeTruthy();
    });

    it("promises no final invoice", () => {
      render(<PaygPlanSection />);

      expect(screen.queryByText(/final invoice/i)).toBeNull();
      expect(screen.getByText(/nothing to invoice/i)).toBeTruthy();
    });

    it("offers resume in place of cancellation", () => {
      render(<PaygPlanSection />);

      expect(resumeButton()).not.toBeNull();
      expect(cancelTrigger()).toBeNull();
    });
  });

  // Past invoices outlive the subscription, so the portal stays reachable even
  // when there is no lifecycle left to act on.
  it.each<StripeSubscription["status"]>([
    "canceled",
    "unpaid",
    "incomplete_expired",
  ])("leaves only the portal once the subscription is %s", (status) => {
    queryState({ data: subscription({ status }) });

    render(<PaygPlanSection />);

    expect(screen.getByText(/pay as you go — ended/i)).toBeTruthy();
    expect(portalButton()).not.toBeNull();
    expect(cancelTrigger()).toBeNull();
    expect(resumeButton()).toBeNull();
  });

  it.each<StripeSubscription["status"]>(["incomplete", "paused"])(
    "sends a %s subscription to the portal to be finished",
    (status) => {
      queryState({ data: subscription({ status }) });

      render(<PaygPlanSection />);

      expect(screen.getByText(/pay as you go — not active/i)).toBeTruthy();
      expect(portalButton()).not.toBeNull();
      expect(cancelTrigger()).toBeNull();
      expect(resumeButton()).toBeNull();
    },
  );

  // Every action here is admin-only at the API, so a member gets the state and
  // no control that would fire a request bound to be refused.
  it("shows a member the plan without any controls", () => {
    mocks.hasAnyScope.mockReturnValue(false);

    render(<PaygPlanSection />);

    expect(screen.getByText("Pay as you go")).toBeTruthy();
    expect(portalButton()).toBeNull();
    expect(cancelTrigger()).toBeNull();
    expect(resumeButton()).toBeNull();
    expect(
      screen.getByText(/only organization admins can manage billing/i),
    ).toBeTruthy();
    expect(mocks.hasAnyScope).toHaveBeenCalledWith(["org:admin"]);
  });

  it("gives an admin the portal alongside the plan state", () => {
    render(<PaygPlanSection />);

    expect(portalButton()).not.toBeNull();
    expect(mocks.portalMutate).not.toHaveBeenCalled();
  });

  // The shared query client throws everything but a 401/403 to the app error
  // boundary, which would take the whole billing page down whenever Stripe is
  // unreachable. Handling the failure inline only works if the read opts out.
  it("keeps a Stripe outage out of the app error boundary", () => {
    render(<PaygPlanSection />);

    const options = mocks.query.mock.calls.at(-1)?.[2] as {
      throwOnError?: boolean;
    };
    expect(options.throwOnError).toBe(false);
  });

  it("shows a content-shaped placeholder while the subscription loads", () => {
    queryState();

    const { container } = render(<PaygPlanSection />);

    expect(heading()).not.toBeNull();
    // The loading branch is the skeleton, not an empty body or a premature
    // "no subscription": one bar per line of the state it is standing in for.
    expect(container.querySelectorAll(".skeleton")).toHaveLength(3);
    expect(screen.queryByRole("alert")).toBeNull();
    expect(portalButton()).toBeNull();
    expect(cancelTrigger()).toBeNull();
  });

  // The pay-as-you-go tier predates Stripe, so an organization can hold it
  // without a Stripe subscription behind it. That 404 is a stable answer, and
  // dressing it up as an outage would leave the admin retrying forever.
  describe("when the organization has no Stripe subscription", () => {
    beforeEach(() => {
      queryState({ isError: true, error: notFound() });
    });

    it("says so instead of reporting an outage", () => {
      render(<PaygPlanSection />);

      expect(screen.getByText(/no stripe subscription/i)).toBeTruthy();
      expect(
        screen.getByText(/no payment method or invoice history/i),
      ).toBeTruthy();
      expect(screen.queryByText(/couldn't load your subscription/i)).toBeNull();
    });

    it("offers nothing to retry", () => {
      render(<PaygPlanSection />);

      expect(screen.queryByRole("button", { name: /^retry$/i })).toBeNull();
      expect(mocks.refetch).not.toHaveBeenCalled();
    });

    // There is no Stripe customer to open a portal for, and no lifecycle to
    // cancel or resume.
    it("offers no billing controls", () => {
      render(<PaygPlanSection />);

      expect(portalButton()).toBeNull();
      expect(cancelTrigger()).toBeNull();
      expect(resumeButton()).toBeNull();
    });

    // The answer is definitive, so it outranks whatever the cache still holds.
    it("outranks a cached subscription", () => {
      queryState({
        data: subscription(),
        isError: true,
        error: notFound(),
      });

      render(<PaygPlanSection />);

      expect(screen.getByText(/no stripe subscription/i)).toBeTruthy();
      expect(screen.queryByText("Pay as you go")).toBeNull();
      expect(cancelTrigger()).toBeNull();
    });
  });

  it("explains a failed load and retries it in place", () => {
    queryState({ isError: true, error: new Error("stripe unavailable") });

    render(<PaygPlanSection />);

    expect(screen.getByRole("alert").textContent).toMatch(
      /couldn't load your subscription/i,
    );

    fireEvent.click(screen.getByRole("button", { name: /^retry$/i }));

    expect(mocks.refetch).toHaveBeenCalledTimes(1);
  });

  it("disables the retry while the reload is in flight", () => {
    queryState({
      isError: true,
      error: new Error("stripe unavailable"),
      isFetching: true,
    });

    render(<PaygPlanSection />);

    const button = screen.getByRole("button", { name: /retrying/i });
    expect(button.hasAttribute("disabled")).toBe(true);
  });

  // A refetch that fails leaves the last successful subscription in the cache,
  // so the query reports data and an error together. The state stays — an
  // admin mid-cancellation shouldn't lose it — and the staleness is called out.
  it("keeps the plan and reports the failure when a cached subscription is held", () => {
    queryState({
      data: subscription(),
      isError: true,
      error: new Error("stripe unavailable"),
    });

    render(<PaygPlanSection />);

    expect(screen.getByText("Pay as you go")).toBeTruthy();
    expect(screen.getByRole("alert").textContent).toMatch(
      /couldn't refresh your subscription/i,
    );
  });

  // The account type can already read as PAYG while a product trial is still
  // running, but checkout hasn't created a Stripe subscription yet — asking
  // for one answers 404, and rendering that as "billing isn't managed through
  // Stripe" would contradict the checkout button sitting right above it.
  describe("during an active product trial", () => {
    beforeEach(() => {
      mocks.session.mockReturnValue({
        trial: {
          startedAt: new Date(Date.now() - 2 * DAY),
          endsAt: new Date(Date.now() + 12 * DAY),
        },
      });
    });

    it("leaves the view to the checkout CTA", () => {
      const { container } = render(<PaygPlanSection />);

      expect(container.innerHTML).toBe("");
      expect(mocks.query).not.toHaveBeenCalled();
    });

    it("never renders the no-subscription copy beside it", () => {
      queryState({ isError: true, error: notFound() });

      render(<PaygPlanSection />);

      expect(screen.queryByText(/no stripe subscription/i)).toBeNull();
      expect(
        screen.queryByText(/billing isn't managed through stripe/i),
      ).toBeNull();
    });

    // Once the trial is over the section takes over from the CTA, on the same
    // clock the CTA and the inference-cap read.
    it("takes over once the trial has ended", () => {
      mocks.session.mockReturnValue({
        trial: {
          startedAt: new Date(Date.now() - 20 * DAY),
          endsAt: new Date(Date.now() - 6 * DAY),
        },
      });

      render(<PaygPlanSection />);

      expect(heading()).not.toBeNull();
      expect(screen.getByText("Pay as you go")).toBeTruthy();
    });
  });

  // Every other tier has no self-serve subscription to report on: the trial
  // tiers get the checkout CTA, and enterprise bills through its contract.
  it.each<ProductTier>([
    "base",
    "base_PAID",
    "__deprecated__pro",
    "enterprise",
  ])("renders nothing on the %s view", (tier) => {
    mocks.productTier.mockReturnValue(tier);

    const { container } = render(<PaygPlanSection />);

    expect(container.innerHTML).toBe("");
    expect(mocks.query).not.toHaveBeenCalled();
  });
});
