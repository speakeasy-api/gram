import type { Scope } from "@gram/client/models/components/rolegrant.js";
import type { StripeSubscription } from "@gram/client/models/components/stripesubscription.js";
import type { ProductTier } from "@/hooks/useProductTier";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  productTier: vi.fn(),
  hasScope: vi.fn(),
  subscription: vi.fn(),
  creditUsage: vi.fn(),
  portalMutate: vi.fn(),
}));

vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => mocks.productTier() as ProductTier,
}));

// The real RequireScope decides what renders, so the gates under test are the
// ones that ship rather than a stand-in for them.
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: Scope) => mocks.hasScope(scope) as boolean,
    hasAnyScope: (scopes: Scope[]) =>
      scopes.some((scope) => mocks.hasScope(scope) as boolean),
    hasAllScopes: (scopes: Scope[]) =>
      scopes.every((scope) => mocks.hasScope(scope) as boolean),
    isLoading: false,
  }),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: vi.fn() }),
}));

// The generated hooks are mocked rather than the shared wrapper: the query
// options are part of what's under test, so they have to be observed where
// they land.
vi.mock("@gram/client/react-query/getStripeSubscription.js", () => ({
  useGetStripeSubscription: (...args: unknown[]) => mocks.subscription(...args),
}));

vi.mock("@gram/client/react-query/getCreditUsage.js", () => ({
  useGetCreditUsage: (...args: unknown[]) => mocks.creditUsage(...args),
}));

vi.mock("@gram/client/react-query/createStripePortalSession.js", () => ({
  useCreateStripePortalSessionMutation: () => ({
    mutate: mocks.portalMutate,
    isPending: false,
  }),
}));

// The route helper resolves the current org from the URL, which there isn't one
// of here. The link is rendered with whatever hash it was handed so the anchor
// it points at is what the test reads.
vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({
    billing: {
      Link: ({ hash, children }: { hash?: string; children: ReactNode }) => (
        <a data-testid="cap-cta" href={`/billing${hash ? `#${hash}` : ""}`}>
          {children}
        </a>
      ),
    },
  }),
}));

import {
  PaygCapPausedBanner,
  PaygPaymentFailedBanner,
} from "./billing-banners";
import { CHAT_SPEND_CAP_ANCHOR, isChatSpendCapReached } from "./chat-spend-cap";

function subscription(
  overrides: Partial<StripeSubscription> = {},
): StripeSubscription {
  return {
    status: "active",
    currentPeriodStart: new Date("2026-08-01T12:00:00.000Z"),
    currentPeriodEnd: new Date("2026-09-01T12:00:00.000Z"),
    cancelAtPeriodEnd: false,
    paymentFailed: false,
    ...overrides,
  };
}

function subscriptionState({
  data,
  error,
}: { data?: StripeSubscription; error?: unknown } = {}) {
  mocks.subscription.mockReturnValue({ data, error });
}

function creditUsageState(data?: {
  monthlyCredits: number;
  creditsUsed: number;
}) {
  mocks.creditUsage.mockReturnValue({ data });
}

/** Shaped like the SDK's 404 rejection, which is what the branch keys on. */
function notFound(): Error {
  return Object.assign(new Error("subscription not found"), {
    statusCode: 404,
  });
}

function unreachable(): Error {
  return Object.assign(new Error("stripe unreachable"), { statusCode: 503 });
}

// A failed payment is announced; a pause the organization set for itself is
// reported. Each banner is the only node in the tree carrying its role.
const paymentBanner = () => screen.queryByRole("alert");

const capBanner = () => screen.queryByRole("status");

const updatePaymentButton = () =>
  screen.queryByRole("button", { name: /update payment method/i });

const POLLING_OPTIONS = {
  refetchInterval: 5 * 60 * 1000,
  refetchIntervalInBackground: false,
  refetchOnWindowFocus: true,
  throwOnError: false,
};

describe("isChatSpendCapReached", () => {
  // A cap of zero is "none configured", not "spend nothing" — the endpoint
  // reports it for organizations that never set one.
  const cases: Array<{
    name: string;
    usage: { monthlyCredits: number; creditsUsed: number } | undefined;
    reached: boolean;
  }> = [
    { name: "nothing loaded", usage: undefined, reached: false },
    {
      name: "no cap configured and nothing spent",
      usage: { monthlyCredits: 0, creditsUsed: 0 },
      reached: false,
    },
    {
      name: "no cap configured but spending",
      usage: { monthlyCredits: 0, creditsUsed: 42 },
      reached: false,
    },
    {
      name: "under the cap",
      usage: { monthlyCredits: 100, creditsUsed: 99 },
      reached: false,
    },
    {
      name: "exactly at the cap",
      usage: { monthlyCredits: 100, creditsUsed: 100 },
      reached: true,
    },
    {
      name: "over the cap",
      usage: { monthlyCredits: 100, creditsUsed: 150 },
      reached: true,
    },
  ];

  for (const { name, usage, reached } of cases) {
    it(`${reached ? "pauses" : "runs"} with ${name}`, () => {
      expect(isChatSpendCapReached(usage)).toBe(reached);
    });
  }
});

describe("PaygCapPausedBanner", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.productTier.mockReturnValue("payg");
    mocks.hasScope.mockReturnValue(true);
    creditUsageState({ monthlyCredits: 100, creditsUsed: 100 });
  });

  afterEach(cleanup);

  // The banner rides the page header, so it renders on every page in the app.
  // A tier with no pay-as-you-go bill has no cap to reach, and asking anyway
  // would put a request behind every page load for every one of them.
  it.each(["base", "base_PAID", "__deprecated__pro", "enterprise"] as const)(
    "reads nothing on the %s tier",
    (tier) => {
      mocks.productTier.mockReturnValue(tier);

      render(<PaygCapPausedBanner />);

      expect(capBanner()).toBeNull();
      expect(mocks.creditUsage).not.toHaveBeenCalled();
    },
  );

  it("reads nothing without org:read", () => {
    mocks.hasScope.mockReturnValue(false);

    render(<PaygCapPausedBanner />);

    expect(capBanner()).toBeNull();
    expect(mocks.creditUsage).not.toHaveBeenCalled();
  });

  it("stays out of the way while spend is under the cap", () => {
    creditUsageState({ monthlyCredits: 100, creditsUsed: 99 });

    render(<PaygCapPausedBanner />);

    expect(capBanner()).toBeNull();
  });

  // Chat stops at the cap, not past it, so the boundary itself is paused.
  it.each([100, 150])(
    "reports the pause with %i of 100 credits used",
    (creditsUsed) => {
      creditUsageState({ monthlyCredits: 100, creditsUsed });

      render(<PaygCapPausedBanner />);

      expect(capBanner()).not.toBeNull();
    },
  );

  // The banner is the only place the pause is explained, so it has to land on
  // the field that ends it rather than the top of the billing page.
  it("links to the spend cap editor", () => {
    render(<PaygCapPausedBanner />);

    expect(screen.getByTestId("cap-cta").getAttribute("href")).toBe(
      `/billing#${CHAT_SPEND_CAP_ANCHOR}`,
    );
  });

  it("clears once the cap is raised above what's been spent", () => {
    const { rerender } = render(<PaygCapPausedBanner />);
    expect(capBanner()).not.toBeNull();

    creditUsageState({ monthlyCredits: 250, creditsUsed: 100 });
    rerender(<PaygCapPausedBanner />);

    expect(capBanner()).toBeNull();
  });

  // Nothing the user does here changes the answer, so the only way the banner
  // ever appears is a poll. A tab nobody is looking at can't show one.
  it("polls a visible tab and leaves a hidden one alone", () => {
    render(<PaygCapPausedBanner />);

    expect(mocks.creditUsage).toHaveBeenCalledWith(
      undefined,
      undefined,
      expect.objectContaining(POLLING_OPTIONS),
    );
  });

  // A read that fails leaves nothing to report, and a banner is not the place
  // to report an outage — it would appear on every page in the app.
  it("says nothing when the usage read fails with nothing cached", () => {
    creditUsageState(undefined);

    render(<PaygCapPausedBanner />);

    expect(capBanner()).toBeNull();
  });
});

describe("PaygPaymentFailedBanner", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.productTier.mockReturnValue("payg");
    mocks.hasScope.mockReturnValue(true);
    subscriptionState({ data: subscription({ paymentFailed: true }) });
  });

  afterEach(cleanup);

  it.each(["base", "base_PAID", "__deprecated__pro", "enterprise"] as const)(
    "reads nothing on the %s tier",
    (tier) => {
      mocks.productTier.mockReturnValue(tier);

      render(<PaygPaymentFailedBanner />);

      expect(paymentBanner()).toBeNull();
      expect(mocks.subscription).not.toHaveBeenCalled();
    },
  );

  it("reads nothing without org:read", () => {
    mocks.hasScope.mockReturnValue(false);

    render(<PaygPaymentFailedBanner />);

    expect(paymentBanner()).toBeNull();
    expect(mocks.subscription).not.toHaveBeenCalled();
  });

  it("reports a live failed payment", () => {
    render(<PaygPaymentFailedBanner />);

    expect(paymentBanner()).not.toBeNull();
  });

  it("clears once a payment lands", () => {
    const { rerender } = render(<PaygPaymentFailedBanner />);
    expect(paymentBanner()).not.toBeNull();

    subscriptionState({ data: subscription({ paymentFailed: false }) });
    rerender(<PaygPaymentFailedBanner />);

    expect(paymentBanner()).toBeNull();
  });

  // A 404 is an answer, not an outage: a subscription that has gone away can't
  // have a payment failing against it, whatever the cache still holds.
  it("hides on a 404 even with a failed payment cached", () => {
    subscriptionState({
      data: subscription({ paymentFailed: true }),
      error: notFound(),
    });

    render(<PaygPaymentFailedBanner />);

    expect(paymentBanner()).toBeNull();
  });

  // Every other failure says nothing about the payment, and the last thing
  // Stripe did say was that it failed. Dropping the banner would tell an admin
  // the problem went away.
  it("keeps reporting a cached failed payment through a transient error", () => {
    subscriptionState({
      data: subscription({ paymentFailed: true }),
      error: unreachable(),
    });

    render(<PaygPaymentFailedBanner />);

    expect(paymentBanner()).not.toBeNull();
  });

  it("says nothing when the read fails with nothing cached", () => {
    subscriptionState({ error: unreachable() });

    render(<PaygPaymentFailedBanner />);

    expect(paymentBanner()).toBeNull();
  });

  it("polls a visible tab and leaves a hidden one alone", () => {
    render(<PaygPaymentFailedBanner />);

    expect(mocks.subscription).toHaveBeenCalledWith(
      undefined,
      undefined,
      expect.objectContaining(POLLING_OPTIONS),
    );
  });

  it("hands an admin the portal that takes a new card", () => {
    render(<PaygPaymentFailedBanner />);

    expect(updatePaymentButton()).not.toBeNull();
  });

  // The portal is admin-only at the API, so offering it to a member would only
  // invite a request that is going to be refused. They still need to know why
  // service is about to stop, and who can fix it.
  it("tells a member who can fix it instead of offering the portal", () => {
    mocks.hasScope.mockImplementation((scope: Scope) => scope === "org:read");

    render(<PaygPaymentFailedBanner />);

    expect(paymentBanner()).not.toBeNull();
    expect(updatePaymentButton()).toBeNull();
    expect(
      screen.getByText(/ask an organization admin to update the payment/i),
    ).toBeTruthy();
  });
});

// The two states are unrelated — a card can fail in a month that also hit the
// cap — so neither banner suppresses the other. A failed payment stops the
// whole account, so it reads first.
describe("both billing banners at once", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.productTier.mockReturnValue("payg");
    mocks.hasScope.mockReturnValue(true);
    subscriptionState({ data: subscription({ paymentFailed: true }) });
    creditUsageState({ monthlyCredits: 100, creditsUsed: 100 });
  });

  afterEach(cleanup);

  it("shows both, payment first", () => {
    render(
      <>
        <PaygPaymentFailedBanner />
        <PaygCapPausedBanner />
      </>,
    );

    const payment = paymentBanner();
    const cap = capBanner();
    expect(payment).not.toBeNull();
    expect(cap).not.toBeNull();

    // Node.DOCUMENT_POSITION_FOLLOWING — the cap banner comes after.
    expect(payment!.compareDocumentPosition(cap!) & 4).toBe(4);
  });
});
