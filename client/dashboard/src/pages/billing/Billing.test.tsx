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

// The usage meters and the TUM view own their own data; this test is only
// about which sections the page reaches for a given tier.
vi.mock("@gram/client/react-query/getCreditUsage.js", () => ({
  useGetCreditUsage: () => ({ data: undefined }),
}));
vi.mock("@gram/client/react-query/getPeriodUsage.js", () => ({
  useGetPeriodUsage: () => ({ data: undefined }),
}));
vi.mock("@gram/client/react-query/getUsageTiers.js", () => ({
  useGetUsageTiers: () => ({ data: undefined, isLoading: false }),
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
