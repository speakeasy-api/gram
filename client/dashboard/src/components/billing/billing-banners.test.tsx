import type { InferenceSpendCap } from "@gram/client/models/components/inferencespendcap.js";
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
  inferenceCaps: vi.fn(),
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

vi.mock("@gram/client/react-query/getInferenceSpendCaps.js", () => ({
  useGetInferenceSpendCaps: (...args: unknown[]) =>
    mocks.inferenceCaps(...args),
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
  PaygCapReachedBanners,
  PaygPaymentFailedBanner,
} from "./billing-banners";
import { inferenceCapAnchor } from "./inference-caps";

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

function cap(overrides: Partial<InferenceSpendCap> = {}): InferenceSpendCap {
  return {
    keyType: "chat",
    creditsUsed: 100,
    monthlyCredits: 100,
    disabled: false,
    ...overrides,
  };
}

/** The list as the endpoint reports it: this org's materialized Gram keys. */
function inferenceCapsState(data?: InferenceSpendCap[]) {
  mocks.inferenceCaps.mockReturnValue({ data });
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
// reported. The payment banner is the only node in the tree carrying `alert`;
// the cap banners are one `status` each, since a month can reach both caps.
const paymentBanner = () => screen.queryByRole("alert");

const capBanners = () => screen.queryAllByRole("status");

const capCtaHrefs = () =>
  screen.queryAllByTestId("cap-cta").map((link) => link.getAttribute("href"));

const updatePaymentButton = () =>
  screen.queryByRole("button", { name: /update payment method/i });

const POLLING_OPTIONS = {
  refetchInterval: 5 * 60 * 1000,
  refetchIntervalInBackground: false,
  refetchOnWindowFocus: true,
  throwOnError: false,
};

describe("PaygCapReachedBanners", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.productTier.mockReturnValue("payg");
    mocks.hasScope.mockReturnValue(true);
    inferenceCapsState([cap({ keyType: "chat" })]);
  });

  afterEach(cleanup);

  // The banners ride the page header, so they render on every page in the app.
  // A tier with no pay-as-you-go bill has no cap to reach, and asking anyway
  // would put a request behind every page load for every one of them.
  it.each(["base", "base_PAID", "__deprecated__pro", "enterprise"] as const)(
    "reads nothing on the %s tier",
    (tier) => {
      mocks.productTier.mockReturnValue(tier);

      render(<PaygCapReachedBanners />);

      expect(capBanners()).toHaveLength(0);
      expect(mocks.inferenceCaps).not.toHaveBeenCalled();
    },
  );

  it("reads nothing without org:read", () => {
    mocks.hasScope.mockReturnValue(false);

    render(<PaygCapReachedBanners />);

    expect(capBanners()).toHaveLength(0);
    expect(mocks.inferenceCaps).not.toHaveBeenCalled();
  });

  it("stays out of the way while spend is under the cap", () => {
    inferenceCapsState([cap({ creditsUsed: 99, monthlyCredits: 100 })]);

    render(<PaygCapReachedBanners />);

    expect(capBanners()).toHaveLength(0);
  });

  // Inference stops at the cap, not past it, so the boundary itself is paused.
  it.each([100, 150])(
    "reports the pause with %i of 100 credits used",
    (creditsUsed) => {
      inferenceCapsState([cap({ creditsUsed, monthlyCredits: 100 })]);

      render(<PaygCapReachedBanners />);

      expect(capBanners()).toHaveLength(1);
    },
  );

  // Each banner names the cap it is about: the caps limit unrelated work, and
  // an admin sent to raise the wrong one has learned nothing.
  it.each<[InferenceSpendCap["keyType"], string, string]>([
    ["chat", "Other inference cap reached", "other"],
    ["internal", "Security inference cap reached", "security"],
  ])("names the %s cap it is reporting", (keyType, title, slug) => {
    inferenceCapsState([cap({ keyType })]);

    render(<PaygCapReachedBanners />);

    expect(screen.getByText(title)).toBeTruthy();
    expect(capCtaHrefs()).toEqual([`/billing#${inferenceCapAnchor(keyType)}`]);
    expect(inferenceCapAnchor(keyType)).toContain(slug);
  });

  // The banners are exactly the reached caps in the list, so one is never
  // conjured for a key the endpoint didn't return.
  it("reports only the caps the endpoint returned", () => {
    inferenceCapsState([cap({ keyType: "internal" })]);

    render(<PaygCapReachedBanners />);

    expect(capBanners()).toHaveLength(1);
    expect(screen.queryByText(/other inference cap reached/i)).toBeNull();
  });

  it("says nothing when the list is empty", () => {
    inferenceCapsState([]);

    render(<PaygCapReachedBanners />);

    expect(capBanners()).toHaveLength(0);
  });

  // The two caps are independent, so a month can reach both. Neither notice
  // suppresses the other, and the invoiced one reads first.
  it("shows one banner per cap reached, invoiced first", () => {
    inferenceCapsState([
      cap({ keyType: "internal" }),
      cap({ keyType: "chat" }),
    ]);

    render(<PaygCapReachedBanners />);

    const banners = capBanners();
    expect(banners).toHaveLength(2);
    expect(banners[0]!.textContent).toMatch(/other inference cap reached/i);
    expect(banners[1]!.textContent).toMatch(/security inference cap reached/i);
    expect(capCtaHrefs()).toEqual([
      `/billing#${inferenceCapAnchor("chat")}`,
      `/billing#${inferenceCapAnchor("internal")}`,
    ]);
  });

  it("reports only the cap that was reached when the other is under", () => {
    inferenceCapsState([
      cap({ keyType: "chat", creditsUsed: 10, monthlyCredits: 100 }),
      cap({ keyType: "internal" }),
    ]);

    render(<PaygCapReachedBanners />);

    expect(capBanners()).toHaveLength(1);
    expect(capCtaHrefs()).toEqual([
      `/billing#${inferenceCapAnchor("internal")}`,
    ]);
  });

  it("tells a member who can raise the cap instead of linking the control", () => {
    mocks.hasScope.mockImplementation((scope: Scope) => scope === "org:read");

    render(<PaygCapReachedBanners />);

    expect(capBanners()).toHaveLength(1);
    expect(capCtaHrefs()).toHaveLength(0);
    expect(
      screen.getByText(/ask an organization admin to raise this cap/i),
    ).toBeTruthy();
  });

  it("clears once the cap is raised above what's been spent", () => {
    const { rerender } = render(<PaygCapReachedBanners />);
    expect(capBanners()).toHaveLength(1);

    inferenceCapsState([cap({ creditsUsed: 100, monthlyCredits: 250 })]);
    rerender(<PaygCapReachedBanners />);

    expect(capBanners()).toHaveLength(0);
  });

  // Nothing the user does here changes the answer, so the only way a banner
  // ever appears is a poll. A tab nobody is looking at can't show one.
  it("polls a visible tab and leaves a hidden one alone", () => {
    render(<PaygCapReachedBanners />);

    expect(mocks.inferenceCaps).toHaveBeenCalledWith(
      undefined,
      undefined,
      expect.objectContaining(POLLING_OPTIONS),
    );
  });

  // A read that fails leaves nothing to report, and a banner is not the place
  // to report an outage — it would appear on every page in the app.
  it("says nothing when the cap read fails with nothing cached", () => {
    inferenceCapsState(undefined);

    render(<PaygCapReachedBanners />);

    expect(capBanners()).toHaveLength(0);
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

// The states are unrelated — a card can fail in a month that also reached both
// caps — so no banner suppresses another. A failed payment stops the whole
// account, so it reads first.
describe("every billing banner at once", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.productTier.mockReturnValue("payg");
    mocks.hasScope.mockReturnValue(true);
    subscriptionState({ data: subscription({ paymentFailed: true }) });
    inferenceCapsState([
      cap({ keyType: "chat" }),
      cap({ keyType: "internal" }),
    ]);
  });

  afterEach(cleanup);

  it("shows all three, payment first", () => {
    render(
      <>
        <PaygPaymentFailedBanner />
        <PaygCapReachedBanners />
      </>,
    );

    const payment = paymentBanner();
    const caps = capBanners();
    expect(payment).not.toBeNull();
    expect(caps).toHaveLength(2);

    // Node.DOCUMENT_POSITION_FOLLOWING — the cap banners come after.
    for (const banner of caps) {
      expect(payment!.compareDocumentPosition(banner) & 4).toBe(4);
    }
  });
});
