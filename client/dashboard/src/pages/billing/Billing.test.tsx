import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import type { ProductTier } from "@/hooks/useProductTier";

const mocks = vi.hoisted(() => ({
  productTier: vi.fn(),
  flagResult: vi.fn(),
  hasScope: vi.fn(),
  session: vi.fn(),
  subscription: vi.fn(),
  periodUsage: vi.fn(),
  inferenceCaps: vi.fn(),
  usageTiers: vi.fn(),
}));

vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => mocks.productTier() as ProductTier,
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => mocks.flagResult() as FeatureFlagResult,
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: () => mocks.hasScope() as boolean }),
}));

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => false,
  useSession: () => mocks.session(),
}));

vi.mock("@/contexts/Sdk", () => ({ useSdkClient: () => ({ usage: {} }) }));
vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: vi.fn() }),
}));

vi.mock("@gram/client/react-query/createStripeCheckout.js", () => ({
  useCreateStripeCheckoutMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));

// The plan section and the spend cap both read the live subscription; the
// shared wrapper is stubbed so this file stays about which sections the page
// composes for a given tier.
vi.mock("@/components/billing/use-stripe-subscription", () => ({
  useStripeSubscription: () => mocks.subscription(),
}));

vi.mock("@gram/client/react-query/createStripePortalSession.js", () => ({
  useCreateStripePortalSessionMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}));
vi.mock("@gram/client/react-query/cancelStripeSubscription.js", () => ({
  useCancelStripeSubscriptionMutation: () => ({
    mutate: vi.fn(),
    reset: vi.fn(),
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/resumeStripeSubscription.js", () => ({
  useResumeStripeSubscriptionMutation: () => ({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/getStripeSubscription.js", () => ({
  invalidateAllGetStripeSubscription: vi.fn(),
}));

// The usage meters and the TUM view own their own data; this test is only
// about which sections the page reaches for a given tier.
vi.mock("@gram/client/react-query/getCreditUsage.js", () => ({
  useGetCreditUsage: () => ({ data: undefined }),
  invalidateAllGetCreditUsage: vi.fn(),
}));
// The inference caps back both the pay-as-you-go meters and the caps section,
// so which tiers reach them is part of what these tests are about.
vi.mock("@gram/client/react-query/getInferenceSpendCaps.js", () => ({
  useGetInferenceSpendCaps: () =>
    mocks.inferenceCaps() as { data: undefined; isError: boolean },
  invalidateAllGetInferenceSpendCaps: vi.fn(),
}));
vi.mock("@gram/client/react-query/setSpendCap.js", () => ({
  useSetSpendCapMutation: () => ({
    mutate: vi.fn(),
    reset: vi.fn(),
    isPending: false,
    isSuccess: false,
    isError: false,
  }),
}));
// Polar period usage bills nothing for pay as you go, so which tiers reach it
// is part of what these tests are about — the call is recorded, not dropped.
vi.mock("@gram/client/react-query/getPeriodUsage.js", () => ({
  useGetPeriodUsage: () => mocks.periodUsage() as { data: undefined },
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/getUsageTiers.js", () => ({
  useGetUsageTiers: (...args: unknown[]) => mocks.usageTiers(...args),
}));
// The billing email section renders for real here — which tiers reach it is
// exactly what these tests are about — so only its own endpoints are stubbed.
vi.mock("@gram/client/react-query/getBillingEmail.js", () => ({
  useGetBillingEmail: () => ({ data: { email: undefined }, isError: false }),
  invalidateAllGetBillingEmail: vi.fn(),
}));
vi.mock("@gram/client/react-query/setBillingEmail.js", () => ({
  useSetBillingEmailMutation: () => ({
    mutate: vi.fn(),
    reset: vi.fn(),
    isPending: false,
    isSuccess: false,
    isError: false,
  }),
}));
vi.mock("@/components/billing/billing-position-section", () => ({
  BillingPositionSection: () => <div>billing position</div>,
}));
vi.mock("@/components/billing/meter-usage-section", () => ({
  MeterUsageSection: () => <div>meter usage</div>,
}));
vi.mock("@/components/billing/tum-admin-section", () => ({
  TumAdminSection: () => <div>tum admin</div>,
}));

// Banner behavior is covered in billing-banners.test.tsx. This page test owns
// their placement and destructive-before-warning order.
vi.mock("@/components/billing/billing-banners", () => ({
  PaygPaymentFailedBanner: () => <div data-testid="payment-banner" />,
  PaygCapReachedBanners: () => <div data-testid="cap-banner" />,
}));

// Scope gating is exercised by the CTA's own RBAC check; the page frame here
// just has to render its children.
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@/components/page-layout", () => {
  const Header = ({ children }: { children?: ReactNode }) => <>{children}</>;
  Header.Breadcrumbs = () => null;
  const Section = ({ children }: { children: ReactNode }) => <>{children}</>;
  Section.Title = ({ children }: { children: ReactNode }) => (
    <h2>{children}</h2>
  );
  Section.Description = ({ children }: { children: ReactNode }) => (
    <p>{children}</p>
  );
  Section.Body = ({ children }: { children: ReactNode }) => <>{children}</>;
  Section.CTA = ({ children }: { children: ReactNode }) => <>{children}</>;
  const Page = ({ children }: { children: ReactNode }) => <>{children}</>;
  Page.Header = Header;
  Page.Banner = ({ children }: { children: ReactNode }) => <>{children}</>;
  Page.Body = ({ children }: { children: ReactNode }) => <>{children}</>;
  Page.Section = Section;
  return { Page };
});

import Billing from "./Billing";

const DAY = 24 * 60 * 60 * 1000;

const cta = () => screen.queryByRole("button", { name: /add payment method/i });

const billingEmailField = () =>
  screen.queryByLabelText(/billing notification email/i);

const inferenceCapsSection = () =>
  screen.queryByRole("heading", { name: /inference caps/i });

const paymentSection = () =>
  screen.queryByRole("heading", { name: /^payment$/i });

const meterUsageSection = () => screen.queryByText("meter usage");

const polarUsageSection = () =>
  screen.queryByText(/summary of your organization's usage this period/i);

const portalButton = () =>
  screen.queryByRole("button", { name: "Manage billing" });

/**
 * The billing email section invalidates its query through the client, and the
 * inference caps section reads the location to answer an anchor link into it.
 */
function renderBilling() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter>
        <Billing />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("Billing", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.productTier.mockReturnValue("base");
    mocks.flagResult.mockReturnValue({ status: "enabled" });
    mocks.hasScope.mockReturnValue(true);
    mocks.inferenceCaps.mockReturnValue({ data: undefined, isError: false });
    mocks.session.mockReturnValue({
      trial: {
        startedAt: new Date(Date.now() - 2 * DAY),
        endsAt: new Date(Date.now() + 12 * DAY),
      },
    });
    mocks.subscription.mockReturnValue({
      data: {
        status: "active",
        cancelAtPeriodEnd: false,
        paymentFailed: false,
      },
      isError: false,
      isFetching: false,
      refetch: vi.fn(),
    });
    mocks.periodUsage.mockReturnValue({ data: undefined });
    const legacyTierLimits = {
      basePrice: 0,
      includedToolCalls: 0,
      includedServers: 0,
      includedCredits: 0,
      pricePerAdditionalToolCall: 0,
      pricePerAdditionalServer: 0,
      featureBullets: [],
      includedBullets: [],
    };
    mocks.usageTiers.mockReturnValue({
      data: {
        free: legacyTierLimits,
        pro: legacyTierLimits,
        enterprise: legacyTierLimits,
        payg: {
          ...legacyTierLimits,
          featureBullets: ["Enterprise feature set"],
          includedBullets: [
            "Other inference billed at provider cost",
            "Platform-initiated inference billed at provider cost",
          ],
          tumPricePerMillionUsd: "0.35",
        },
      },
      isLoading: false,
      isError: false,
    });
  });

  it("places payment failure before the spend cap warning", () => {
    renderBilling();

    const payment = screen.getByTestId("payment-banner");
    const cap = screen.getByTestId("cap-banner");
    expect(
      payment.compareDocumentPosition(cap) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  });

  afterEach(cleanup);

  it("offers pay as you go to a trialing admin on the self-serve view", () => {
    renderBilling();

    expect(paymentSection()).not.toBeNull();
    expect(cta()).not.toBeNull();
    // The rates for the plan the CTA starts sit on this branch too.
    expect(screen.getByText("Pay as you go pricing")).toBeTruthy();
  });

  // Trials run on the enterprise tier, which short-circuits into the TUM view
  // before the self-serve sections — the CTA has to survive that early return.
  it("offers pay as you go on the enterprise TUM view", () => {
    mocks.productTier.mockReturnValue("enterprise");

    renderBilling();

    expect(screen.getByText("meter usage")).toBeTruthy();
    expect(cta()).not.toBeNull();
    expect(screen.getByText("Pay as you go pricing")).toBeTruthy();
    expect(screen.getByText("$0.35 per million tokens")).toBeTruthy();
    // The pricing section states rates; the plan's product features are not
    // one of them.
    expect(screen.queryByText("Enterprise feature set")).toBeNull();
    expect(mocks.usageTiers).toHaveBeenCalledWith({ throwOnError: false });
  });

  it("shows no checkout CTA to a member", () => {
    mocks.productTier.mockReturnValue("enterprise");
    mocks.hasScope.mockReturnValue(false);

    renderBilling();

    expect(screen.getByText("meter usage")).toBeTruthy();
    expect(cta()).toBeNull();
  });

  // Billing notifications are a pay-as-you-go concern: enterprise orgs are
  // billed through their contract, and the pre-checkout tiers have no bill.
  it("offers billing notification settings on the pay as you go view", () => {
    mocks.productTier.mockReturnValue("payg");

    renderBilling();

    expect(billingEmailField()).not.toBeNull();
  });

  it.each<ProductTier>(["base", "enterprise"])(
    "shows no billing notification settings on the %s view",
    (tier) => {
      mocks.productTier.mockReturnValue(tier);

      renderBilling();

      expect(billingEmailField()).toBeNull();
    },
  );

  // The inference caps are a pay-as-you-go control. A trialing enterprise org
  // is on its way onto PAYG, so it gets them locked rather than hidden — and
  // the TUM early return is the path that org takes.
  it.each<ProductTier>(["payg", "enterprise"])(
    "places the inference caps on the %s view",
    (tier) => {
      mocks.productTier.mockReturnValue(tier);

      renderBilling();

      expect(inferenceCapsSection()).not.toBeNull();
    },
  );

  // The payment section is where a converted organization manages its card,
  // invoices, and cancellation, so it belongs to the pay-as-you-go view only.
  it("places the subscription controls on the payg view", () => {
    mocks.productTier.mockReturnValue("payg");
    mocks.session.mockReturnValue({ trial: null });

    renderBilling();

    expect(paymentSection()).not.toBeNull();
    expect(portalButton()).not.toBeNull();
  });

  // The account type can read as PAYG while a product trial is still running.
  // Checkout is what creates the subscription, so the payment section holds
  // the CTA — subscription controls beside it would be reporting on something
  // that doesn't exist.
  it("keeps a trialing payg org's payment section on the checkout CTA", () => {
    mocks.productTier.mockReturnValue("payg");

    renderBilling();

    expect(paymentSection()).not.toBeNull();
    expect(cta()).not.toBeNull();
    expect(portalButton()).toBeNull();
  });

  it.each<ProductTier>([
    "base",
    "base_PAID",
    "__deprecated__pro",
    "enterprise",
  ])("shows no payment section on the converted %s view", (tier) => {
    mocks.productTier.mockReturnValue(tier);
    mocks.session.mockReturnValue({ trial: null });

    renderBilling();

    expect(paymentSection()).toBeNull();
  });

  // A pre-card trial converts through the checkout CTA, not the subscription
  // controls: there is no subscription to manage until checkout creates one.
  it("keeps the trialing payment section on the checkout CTA", () => {
    mocks.productTier.mockReturnValue("enterprise");

    renderBilling();

    expect(paymentSection()).not.toBeNull();
    expect(cta()).not.toBeNull();
    expect(portalButton()).toBeNull();
  });

  it("shows no inference caps on the pre-checkout view", () => {
    mocks.productTier.mockReturnValue("base");

    renderBilling();

    expect(inferenceCapsSection()).toBeNull();
    // A tier with no pay-as-you-go bill has nothing to cap, so it never asks.
    expect(mocks.inferenceCaps).not.toHaveBeenCalled();
  });

  it("shows no inference caps to enterprise without an active trial", () => {
    mocks.productTier.mockReturnValue("enterprise");
    mocks.session.mockReturnValue({ trial: null });

    renderBilling();

    expect(screen.getByText("meter usage")).toBeTruthy();
    expect(inferenceCapsSection()).toBeNull();
    expect(mocks.inferenceCaps).not.toHaveBeenCalled();
  });

  it("separates the billing position from meter usage on the payg view", () => {
    mocks.productTier.mockReturnValue("payg");
    mocks.session.mockReturnValue({ trial: null });

    renderBilling();

    expect(meterUsageSection()).not.toBeNull();
    expect(screen.getByText("billing position")).toBeTruthy();
    expect(polarUsageSection()).toBeNull();
    expect(mocks.periodUsage).not.toHaveBeenCalled();
  });

  it.each<ProductTier>(["base", "base_PAID", "__deprecated__pro"])(
    "keeps the %s view on the existing usage meters",
    (tier) => {
      mocks.productTier.mockReturnValue(tier);

      renderBilling();

      expect(polarUsageSection()).not.toBeNull();
      expect(mocks.periodUsage).toHaveBeenCalled();
      expect(meterUsageSection()).toBeNull();
      expect(mocks.inferenceCaps).not.toHaveBeenCalled();
    },
  );

  it("keeps the enterprise view on meter usage without Polar usage", () => {
    mocks.productTier.mockReturnValue("enterprise");

    renderBilling();

    expect(polarUsageSection()).toBeNull();
    expect(meterUsageSection()).not.toBeNull();
  });

  it("shows no checkout CTA once the trial has ended", () => {
    mocks.session.mockReturnValue({
      trial: {
        startedAt: new Date(Date.now() - 20 * DAY),
        endsAt: new Date(Date.now() - 6 * DAY),
      },
    });

    renderBilling();

    expect(cta()).toBeNull();
    expect(screen.queryByText("Pay as you go pricing")).toBeNull();
  });
});
