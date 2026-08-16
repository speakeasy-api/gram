import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FeatureFlagResult } from "@/hooks/useFeatureFlag";
import type { ProductTier } from "@/hooks/useProductTier";

const mocks = vi.hoisted(() => ({
  productTier: vi.fn(),
  flagResult: vi.fn(),
  hasScope: vi.fn(),
  session: vi.fn(),
  subscription: vi.fn(),
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
vi.mock("@gram/client/react-query/setSpendCap.js", () => ({
  useSetSpendCapMutation: () => ({
    mutate: vi.fn(),
    reset: vi.fn(),
    isPending: false,
    isSuccess: false,
    isError: false,
  }),
}));
vi.mock("@gram/client/react-query/getPeriodUsage.js", () => ({
  useGetPeriodUsage: () => ({ data: undefined }),
}));
vi.mock("@gram/client/react-query/getUsageTiers.js", () => ({
  useGetUsageTiers: () => ({ data: undefined, isLoading: false }),
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
vi.mock("@/components/billing/tum-section", () => ({
  TumUsageSection: () => <div>tum usage</div>,
}));
vi.mock("@/components/billing/tum-admin-section", () => ({
  TumAdminSection: () => <div>tum admin</div>,
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
  Page.Body = ({ children }: { children: ReactNode }) => <>{children}</>;
  Page.Section = Section;
  return { Page };
});

import Billing from "./Billing";

const DAY = 24 * 60 * 60 * 1000;

const cta = () =>
  screen.queryByRole("button", { name: /start pay as you go/i });

const billingEmailField = () =>
  screen.queryByLabelText(/billing notification email/i);

const spendCapSection = () =>
  screen.queryByRole("heading", { name: /chat spend cap/i });

const planSection = () => screen.queryByRole("heading", { name: /^plan$/i });

const portalButton = () =>
  screen.queryByRole("button", { name: /manage payment method and invoices/i });

/** The billing email section invalidates its query through the client. */
function renderBilling() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <Billing />
    </QueryClientProvider>,
  );
}

describe("Billing", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.productTier.mockReturnValue("base");
    mocks.flagResult.mockReturnValue({ status: "enabled" });
    mocks.hasScope.mockReturnValue(true);
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
  });

  afterEach(cleanup);

  it("offers pay as you go to a trialing admin on the self-serve view", () => {
    render(<Billing />);

    expect(cta()).not.toBeNull();
  });

  // Trials run on the enterprise tier, which short-circuits into the TUM view
  // before the self-serve sections — the CTA has to survive that early return.
  it("offers pay as you go on the enterprise TUM view", () => {
    mocks.productTier.mockReturnValue("enterprise");

    render(<Billing />);

    expect(screen.getByText("tum usage")).toBeTruthy();
    expect(cta()).not.toBeNull();
  });

  it("shows no checkout CTA to a member", () => {
    mocks.productTier.mockReturnValue("enterprise");
    mocks.hasScope.mockReturnValue(false);

    render(<Billing />);

    expect(screen.getByText("tum usage")).toBeTruthy();
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

  // The spend cap is a pay-as-you-go control. A trialing enterprise org is on
  // its way onto PAYG, so it gets the cap locked rather than hidden — and the
  // TUM early return is the path that org takes.
  it.each<ProductTier>(["payg", "enterprise"])(
    "places the chat spend cap on the %s view",
    (tier) => {
      mocks.productTier.mockReturnValue(tier);

      renderBilling();

      expect(spendCapSection()).not.toBeNull();
    },
  );

  // The plan section is where a converted organization manages its card,
  // invoices, and cancellation, so it belongs to the pay-as-you-go view only.
  it("places the pay as you go plan on the payg view", () => {
    mocks.productTier.mockReturnValue("payg");
    mocks.session.mockReturnValue({ trial: null });

    renderBilling();

    expect(planSection()).not.toBeNull();
    expect(portalButton()).not.toBeNull();
  });

  // The account type can read as PAYG while a product trial is still running.
  // Checkout is what creates the subscription, so the CTA owns that view — a
  // plan section beside it would be reporting on something that doesn't exist.
  it("keeps a trialing payg org on the checkout CTA alone", () => {
    mocks.productTier.mockReturnValue("payg");

    renderBilling();

    expect(cta()).not.toBeNull();
    expect(planSection()).toBeNull();
    expect(portalButton()).toBeNull();
  });

  it.each<ProductTier>([
    "base",
    "base_PAID",
    "__deprecated__pro",
    "enterprise",
  ])("shows no pay as you go plan on the %s view", (tier) => {
    mocks.productTier.mockReturnValue(tier);

    renderBilling();

    expect(planSection()).toBeNull();
    expect(portalButton()).toBeNull();
  });

  // A pre-card trial converts through the checkout CTA, not the plan section:
  // there is no subscription to manage until checkout creates one.
  it("keeps the trialing view on the checkout CTA", () => {
    mocks.productTier.mockReturnValue("enterprise");

    renderBilling();

    expect(cta()).not.toBeNull();
    expect(planSection()).toBeNull();
  });

  it("shows no chat spend cap on the pre-checkout view", () => {
    mocks.productTier.mockReturnValue("base");

    renderBilling();

    expect(spendCapSection()).toBeNull();
  });

  it("shows no chat spend cap to enterprise without an active trial", () => {
    mocks.productTier.mockReturnValue("enterprise");
    mocks.session.mockReturnValue({ trial: null });

    renderBilling();

    expect(screen.getByText("tum usage")).toBeTruthy();
    expect(spendCapSection()).toBeNull();
  });

  it("shows no checkout CTA once the trial has ended", () => {
    mocks.session.mockReturnValue({
      trial: {
        startedAt: new Date(Date.now() - 20 * DAY),
        endsAt: new Date(Date.now() - 6 * DAY),
      },
    });

    render(<Billing />);

    expect(cta()).toBeNull();
  });
});
