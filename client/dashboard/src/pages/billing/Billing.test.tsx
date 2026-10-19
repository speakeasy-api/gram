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
  useSession: () => ({
    organization: { slug: "test-org" },
    rawGramAccountType: mocks.productTier(),
    ...mocks.session(),
  }),
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
  useStripeSubscription: (options: unknown) => mocks.subscription(options),
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

// Each billing section owns its data; this test covers which sections the
// page reaches for a given tier.
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
vi.mock("@/components/billing/meter-usage-section", () => ({
  MeterUsageSection: () => <div>meter usage</div>,
}));
vi.mock("@/components/billing/spend-breakdown-section", () => ({
  SpendBreakdownSection: () => <div>spend by product</div>,
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
  Section.Title = ({
    children,
    area,
  }: {
    children: ReactNode;
    area?: string;
  }) => (
    <>
      <span>{area}</span>
      <h2>{children}</h2>
    </>
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

const cta = () => screen.queryByRole("link", { name: /add payment method/i });

const billingEmailField = () =>
  screen.queryByLabelText(/billing notification email/i);

const inferenceCapsSection = () =>
  screen.queryByRole("heading", { name: /inference caps/i });

const paymentSection = () =>
  screen.queryByRole("heading", { name: /^payment$/i });

const meterUsageSection = () => screen.queryByText("meter usage");
const spendBreakdownSection = () => screen.queryByText("spend by product");

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
  const view = () => (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={["/example/billing"]}>
        <Billing />
      </MemoryRouter>
    </QueryClientProvider>
  );
  const result = render(view());
  return { ...result, refresh: () => result.rerender(view()) };
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
          riskScanPricePerMillionUsd: "0.99",
          mcpEgressPricePerGibUsd: "20",
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

  it.each<ProductTier>(["base", "enterprise", "payg"])(
    "links eligible %s trials without payment setup to the dedicated page",
    (tier) => {
      mocks.productTier.mockReturnValue(tier);
      mocks.subscription.mockReturnValue({
        data: undefined,
        error: { statusCode: 404 },
        isError: true,
        isFetching: false,
      });
      renderBilling();
      expect(mocks.subscription).toHaveBeenCalledWith({ enabled: true });
      expect(cta()?.getAttribute("href")).toBe("/test-org/billing/setup");
      expect(screen.getByText(/after your trial ends/)).toBeTruthy();
      expect(paymentSection()).toBeNull();
      expect(screen.queryByText("Pay as you go pricing")).toBeNull();
    },
  );

  it.each(["active", "trialing", "past_due", "incomplete", "canceled"])(
    "does not prompt a trial with an existing %s subscription to add payment",
    (status) => {
      mocks.subscription.mockReturnValue({ data: { status }, isError: false });
      renderBilling();
      expect(cta()).toBeNull();
      expect(paymentSection()).toBeNull();
      expect(screen.queryByText("Pay as you go pricing")).toBeNull();
    },
  );

  it.each([null, { statusCode: 503 }])(
    "does not prompt trials with unknown payment setup %j",
    (error) => {
      mocks.subscription.mockReturnValue({
        data: undefined,
        error,
        isError: error !== null,
        isFetching: error === null,
      });
      renderBilling();
      expect(cta()).toBeNull();
    },
  );

  it.each<ProductTier>(["base_PAID", "__deprecated__pro", "enterprise"])(
    "does not offer setup to nontrial %s even with no Stripe subscription",
    (tier) => {
      mocks.productTier.mockReturnValue(tier);
      mocks.session.mockReturnValue({ trial: null });
      mocks.subscription.mockReturnValue({
        data: undefined,
        error: { statusCode: 404 },
        isError: true,
        isFetching: false,
      });
      renderBilling();
      expect(cta()).toBeNull();
      expect(screen.queryByText("Pay as you go pricing")).toBeNull();
    },
  );

  it.each(["member", "disabled flag"])("hides setup for %s", (reason) => {
    mocks.subscription.mockReturnValue({
      error: { statusCode: 404 },
      isError: true,
    });
    if (reason === "member") mocks.hasScope.mockReturnValue(false);
    else mocks.flagResult.mockReturnValue({ status: "disabled" });
    renderBilling();
    expect(cta()).toBeNull();
    expect(mocks.subscription).toHaveBeenCalledWith({ enabled: false });
  });

  it.each(["free", "base", "payg"])(
    "offers eligible %s setup without a trial only on confirmed absence",
    (tier) => {
      mocks.productTier.mockReturnValue(tier === "free" ? "base" : tier);
      mocks.session.mockReturnValue({
        trial: null,
        rawGramAccountType: tier,
        hasActiveSubscription: false,
      });
      mocks.subscription.mockReturnValue({
        data: undefined,
        error: { statusCode: 404 },
        isError: true,
        isFetching: false,
      });
      renderBilling();
      expect(cta()?.getAttribute("href")).toBe("/test-org/billing/setup");
      expect(paymentSection()).toBeNull();
      expect(screen.queryByText(/after your trial ends/)).toBeNull();
    },
  );

  it.each([null, { statusCode: 503 }])(
    "does not offer setup for unknown subscription state %j",
    (error) => {
      mocks.session.mockReturnValue({
        trial: null,
        rawGramAccountType: "free",
        hasActiveSubscription: false,
      });
      mocks.subscription.mockReturnValue({
        data: undefined,
        error,
        isError: error !== null,
        isFetching: error === null,
      });
      renderBilling();
      expect(paymentSection()).toBeNull();
      expect(cta()).toBeNull();
    },
  );

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
  // is on its way onto PAYG, so it gets them locked rather than hidden.
  it.each<ProductTier>(["payg", "enterprise"])(
    "places the inference caps on the %s view",
    (tier) => {
      mocks.productTier.mockReturnValue(tier);

      renderBilling();

      expect(inferenceCapsSection()).not.toBeNull();
    },
  );

  it("keeps subscription controls and pricing off the paid overview", () => {
    mocks.productTier.mockReturnValue("payg");
    mocks.session.mockReturnValue({ trial: null });
    renderBilling();
    expect(paymentSection()).toBeNull();
    expect(portalButton()).toBeNull();
    expect(cta()).toBeNull();
    expect(screen.queryByText("Pay as you go pricing")).toBeNull();
    expect(meterUsageSection()).not.toBeNull();
  });

  it("prioritizes missing-subscription recovery over stale healthy data", () => {
    mocks.productTier.mockReturnValue("payg");
    mocks.session.mockReturnValue({ trial: null });
    mocks.subscription.mockReturnValue({
      data: { status: "active", paymentFailed: false },
      error: Object.assign(new Error("subscription not found"), {
        statusCode: 404,
      }),
      isError: true,
      isFetching: false,
      refetch: vi.fn(),
    });

    renderBilling();

    expect(cta()).not.toBeNull();
    expect(portalButton()).toBeNull();
    expect(
      cta()!.compareDocumentPosition(meterUsageSection()!) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it.each(["past_due", "incomplete", "canceled", "trialing"])(
    "keeps existing %s subscription recovery on the payment subpage",
    (status) => {
      mocks.productTier.mockReturnValue("payg");
      mocks.session.mockReturnValue({ trial: null });
      mocks.subscription.mockReturnValue({
        data: { status, paymentFailed: true },
        error: new Error("unavailable"),
        isError: true,
      });
      renderBilling();
      expect(portalButton()).toBeNull();
      expect(cta()).toBeNull();
      expect(paymentSection()).toBeNull();
      expect(screen.getByTestId("payment-banner")).toBeTruthy();
    },
  );

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
    expect(cta()).toBeNull();
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

  it.each<ProductTier>(["base", "base_PAID", "__deprecated__pro"])(
    "keeps the %s view on the existing usage meters",
    (tier) => {
      mocks.productTier.mockReturnValue(tier);

      renderBilling();

      expect(polarUsageSection()).not.toBeNull();
      expect(mocks.periodUsage).toHaveBeenCalled();
      expect(meterUsageSection()).toBeNull();
      expect(spendBreakdownSection()).toBeNull();
      expect(mocks.inferenceCaps).not.toHaveBeenCalled();
    },
  );

  it("keeps the enterprise view on meter usage without Polar usage", () => {
    mocks.productTier.mockReturnValue("enterprise");

    renderBilling();

    expect(polarUsageSection()).toBeNull();
    expect(meterUsageSection()).not.toBeNull();
    expect(spendBreakdownSection()).not.toBeNull();
    expect(
      spendBreakdownSection()!.compareDocumentPosition(meterUsageSection()!) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
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

vi.mock("@/components/ui/hooks/useConfig", () => ({
  useConfig: () => ({ theme: "light" }),
}));
