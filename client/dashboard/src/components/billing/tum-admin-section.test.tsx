import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { TokensUnderManagement } from "@gram/client/models/components/tokensundermanagement.js";

const mocks = vi.hoisted(() => ({
  useGetTokensUnderManagement: vi.fn(),
  useSetBillingMetadataMutation: vi.fn(),
}));

vi.mock("@gram/client/react-query/getTokensUnderManagement.js", () => ({
  useGetTokensUnderManagement: mocks.useGetTokensUnderManagement,
  invalidateAllGetTokensUnderManagement: vi.fn(),
}));

vi.mock("@gram/client/react-query/setBillingMetadata.js", () => ({
  useSetBillingMetadataMutation: mocks.useSetBillingMetadataMutation,
}));

// Page chrome isn't what's under test; render it as plain boxes so a failure
// here can only mean the boundary.
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

// Stand in for a chunk that fails to load — the stale-tab-after-deploy case,
// where the hashed asset the page asks for no longer exists. A rejecting
// module factory makes the dynamic import inside React.lazy reject, which is
// exactly what Suspense does NOT catch.
vi.mock("./contract-price-estimator", () => {
  throw new Error("Failed to fetch dynamically imported module");
});

const tum = {
  periodStart: new Date("2026-07-01T00:00:00Z"),
  periodEnd: new Date("2026-08-01T00:00:00Z"),
  tokens: 5_000_000_000,
  monthlyTokenLimit: 30_000_000_000,
  alertEmail: "billing@example.test",
  billingCycleAnchorDay: 1,
  history: [],
} as unknown as TokensUnderManagement;

function renderSection() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  // Imported lazily so the module-level vi.mock calls above are in place.
  return import("./tum-admin-section").then(({ TumAdminSection }) =>
    render(
      <QueryClientProvider client={client}>
        <TumAdminSection />
      </QueryClientProvider>,
    ),
  );
}

describe("TumAdminSection", () => {
  beforeEach(() => {
    mocks.useGetTokensUnderManagement.mockReturnValue({
      data: tum,
      isError: false,
    });
    mocks.useSetBillingMetadataMutation.mockReturnValue({
      mutate: vi.fn(),
      isPending: false,
      isSuccess: false,
      isError: false,
    });
    // React logs the caught error; keep the run readable.
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("keeps the contract form usable when the estimator chunk fails to load", async () => {
    await renderSection();

    // The estimator's failure is contained and explained — and announced,
    // since it lands asynchronously well after the page has settled.
    await waitFor(() => {
      const alert = screen.getByRole("alert");
      expect(alert.textContent).toMatch(/contract estimate couldn't load/i);
    });

    // ...and the form beside it — the thing an admin actually came to change
    // — is still mounted and holding its saved values.
    const limit = screen.getByLabelText(
      /allowed tum per month/i,
    ) as HTMLInputElement;
    expect(limit.value).toBe("30000000000");
    expect(screen.getByText(/save contract terms/i)).toBeTruthy();
  });
});
